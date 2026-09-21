// Webhooks 服务（M5 #12）—— 密钥变更事件对外广播，HMAC-SHA256 签名
//
//	POST   /api/v1/orgs/:slug/webhooks                 创建订阅（owner）
//	GET    /api/v1/orgs/:slug/webhooks                 列表（不含签名密钥）
//	DELETE /api/v1/orgs/:slug/webhooks/:id             删除（owner）
//	GET    /api/v1/orgs/:slug/webhooks/:id/deliveries  投递日志
//
// 投递保障：至少一次 + 指数退避（30s/2m/10m/30m/1h/2h，6 次后 dead）；
// 负载不含明文；签名头 X-Taboo-Signature: t=<unix>,v1=<hex(hmac)>，
// 接收方用共享密钥对 "<t>.<body>" 验签，并校验时间戳窗口（±300s）防重放。
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/httpx"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

// 退避序列（秒）：第 N 次失败后调度第 N 次重试，共 6 次尝试后 dead
var backoffSec = []int{30, 120, 600, 1800, 3600, 7200}
const maxAttempts = 6

// 事件名映射：SecretEvent.Action → webhook 事件类型
var eventNames = map[string]string{
	"created":     "secret.created",
	"updated":     "secret.updated",
	"rolled_back": "secret.rolled_back",
	"deleted":     "secret.deleted",
}

type Service struct {
	DB        *sql.DB
	MasterKey []byte
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func ipOf(r *http.Request) string { return httpx.ClientIP(r) }

// Subscribe 订阅密钥变更事件 → 为每个匹配的订阅生成投递记录（至少一次）
func (s *Service) Subscribe(bus *events.Bus) {
	bus.Subscribe(func(e events.SecretEvent) {
		eventName, ok := eventNames[e.Action]
		if !ok {
			return
		}
		rows, err := s.DB.Query(`SELECT id, secret_enc, events FROM webhooks
			WHERE org_id = ? AND active = 1`, e.OrgID)
		if err != nil {
			return
		}
		type sub struct {
			id, secretEnc, eventsFilter string
		}
		subs := []sub{}
		for rows.Next() {
			var x sub
			if err := rows.Scan(&x.id, &x.secretEnc, &x.eventsFilter); err == nil {
				subs = append(subs, x)
			}
		}
		rows.Close()
		for _, w := range subs {
			if !matchEvents(w.eventsFilter, eventName) {
				continue
			}
			s.enqueue(w.id, e, eventName)
		}
	})
}

// matchEvents 空数组 = 订阅全部事件；否则精确匹配事件名
func matchEvents(filterJSON, eventName string) bool {
	if filterJSON == "" || filterJSON == "[]" {
		return true
	}
	var list []string
	if json.Unmarshal([]byte(filterJSON), &list) != nil {
		return true
	}
	for _, x := range list {
		if x == eventName || x == "*" {
			return true
		}
	}
	return false
}

// enqueue 幂等：同一事件同一订阅只保留一条 pending
func (s *Service) enqueue(webhookID string, e events.SecretEvent, eventName string) {
	payload, _ := json.Marshal(map[string]any{
		"id":        e.EventID,
		"type":      eventName,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"org":       e.OrgID,
		"project":   e.ProjectSlug,
		"env":       e.EnvSlug,
		"folder":    e.Folder,
		"key":       e.Key,
		"version":   e.Version,
		"actor": map[string]string{
			"id": e.ActorID, "name": e.ActorName, "kind": e.ActorKind,
		},
	})
	_, err := s.DB.Exec(`INSERT INTO webhook_deliveries
		(id, webhook_id, org_id, event_id, event, payload, status, next_attempt_at, created_at)
		SELECT ?, ?, ?, ?, ?, ?, 'pending', ?, datetime('now')
		WHERE NOT EXISTS (SELECT 1 FROM webhook_deliveries WHERE webhook_id = ? AND event_id = ? AND status = 'pending')`,
		tc.NewID(), webhookID, e.OrgID, e.EventID, eventName, string(payload),
		time.Now().Unix(), webhookID, e.EventID)
	_ = err
}

// StartWorker 后台投递：轮询到期 pending → POST → 记录结果
func (s *Service) StartWorker(ctx context.Context, interval time.Duration) {
	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			s.drain()
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

func (s *Service) drain() {
	rows, err := s.DB.Query(`SELECT d.id, d.org_id, d.event, d.payload, d.attempts, w.url, w.secret_enc
		FROM webhook_deliveries d JOIN webhooks w ON w.id = d.webhook_id
		WHERE d.status = 'pending' AND w.active = 1 AND d.next_attempt_at <= ?
		ORDER BY d.created_at LIMIT 10`, time.Now().Unix())
	if err != nil {
		return
	}
	type job struct {
		id, orgID, event, payload, url, secretEnc string
		attempts                                  int
	}
	jobs := []job{}
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.orgID, &j.event, &j.payload, &j.attempts, &j.url, &j.secretEnc); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	for _, j := range jobs {
		s.deliver(j.id, j.orgID, j.event, j.payload, j.attempts, j.url, j.secretEnc)
	}
}

// deliver 单次投递尝试；失败按退避序列调度重试，超限 dead
func (s *Service) deliver(id, orgID, event, payload string, attempts int, targetURL, secretEnc string) {
	secret, err := tc.Decrypt(s.MasterKey, secretEnc)
	if err != nil {
		s.finishFail(id, orgID, event, attempts, "decrypt webhook secret failed")
		return
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest("POST", targetURL, bytes.NewBufferString(payload))
	if err != nil {
		s.finishFail(id, orgID, event, attempts, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Taboo-Signature", "t="+ts+",v1="+sig)
	req.Header.Set("X-Taboo-Event", event)
	req.Header.Set("User-Agent", "taboo-webhook/1.0")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		s.finishFail(id, orgID, event, attempts, err.Error())
		return
	}
	defer res.Body.Close()
	if res.StatusCode/100 == 2 {
		_, _ = s.DB.Exec(`UPDATE webhook_deliveries SET status = 'success', http_code = ?,
			attempts = attempts + 1, delivered_at = datetime('now') WHERE id = ?`, res.StatusCode, id)
		auth.Audit(s.DB, orgID, nil, "webhook.deliver", "webhook/"+id,
			map[string]any{"event": event, "http_code": res.StatusCode, "attempts": attempts + 1}, "")
		return
	}
	s.finishFail(id, orgID, event, attempts, "HTTP "+strconv.Itoa(res.StatusCode))
}

func (s *Service) finishFail(id, orgID, event string, attempts int, cause string) {
	attempts++
	status, nextAt := retryAfterFail(attempts)
	if status == "dead" {
		_ = auth.Audit(s.DB, orgID, nil, "webhook.dead", "webhook/"+id,
			map[string]any{"event": event, "error": cause, "attempts": attempts}, "")
	}
	_, _ = s.DB.Exec(`UPDATE webhook_deliveries SET status = ?, attempts = ?, next_attempt_at = ?,
		error = ? WHERE id = ?`, status, attempts, nextAt, cause, id)
}

// ---------- HTTP ----------

func (s *Service) orgOf(w http.ResponseWriter, r *http.Request) (orgID, slug string, ok bool) {
	slug = chi.URLParam(r, "slug")
	if err := s.DB.QueryRow(`SELECT id FROM orgs WHERE slug = ?`, slug).Scan(&orgID); err != nil {
		writeErr(w, apperr.NotFound)
		return "", slug, false
	}
	u := auth.From(r)
	if u.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return "", slug, false
	}
	if _, member := auth.Membership(s.DB, orgID, u.ID); !member {
		writeErr(w, apperr.Forbidden)
		return "", slug, false
	}
	return orgID, slug, true
}

func manageOnly(db *sql.DB, w http.ResponseWriter, r *http.Request, orgID string) bool {
	u := auth.From(r)
	if u.Kind != auth.KindUser || !auth.Can(db, u, orgID, "", rbac.Admin, "") {
		auth.Deny(db, w, r, orgID, rbac.Admin, "org/webhooks")
		return false
	}
	return true
}

// Routes 挂载到 /orgs/{slug}/webhooks
func (s *Service) Routes(r chi.Router) {
	r.Route("/orgs/{slug}/webhooks", func(r chi.Router) {
		r.Get("/", s.List)
		r.Post("/", s.Create)
		r.Delete("/{id}", s.Delete)
		r.Get("/{id}/deliveries", s.Deliveries)
	})
}

type webhookOut struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Active    bool     `json:"active"`
	CreatedAt string   `json:"created_at"`
}

// List GET /
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT id, url, events, active, created_at FROM webhooks
		WHERE org_id = ? ORDER BY created_at`, orgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	out := []webhookOut{}
	for rows.Next() {
		var x webhookOut
		var ev string
		var act int
		if err := rows.Scan(&x.ID, &x.URL, &ev, &act, &x.CreatedAt); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(ev), &x.Events)
		x.Active = act == 1
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"webhooks": out})
}

// Create POST / {url, secret?, events?} —— secret 缺省自动生成，仅本次响应返回
func (s *Service) Create(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !manageOnly(s.DB, w, r, orgID) {
		return
	}
	var b struct {
		URL    string   `json:"url"`
		Secret string   `json:"secret"`
		Events []string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.URL == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if u, err := url.Parse(b.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		writeErr(w, apperr.New(400, "INVALID", "url must be http(s)"))
		return
	}
	if b.Secret == "" {
		b.Secret = tc.NewID() + tc.NewID()
	}
	for _, e := range b.Events {
		if _, ok := eventNames[reverseEventName(e)]; e != "*" && !ok {
			writeErr(w, apperr.New(400, "INVALID", "unknown event: "+e))
			return
		}
	}
	secretEnc, err := tc.Encrypt(s.MasterKey, b.Secret)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	evJSON, _ := json.Marshal(b.Events)
	id := tc.NewID()
	if _, err := s.DB.Exec(`INSERT INTO webhooks (id, org_id, url, secret_enc, events)
		VALUES (?, ?, ?, ?, ?)`, id, orgID, b.URL, secretEnc, string(evJSON)); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, orgID, auth.From(r), "webhook.create", "org/"+slug+"/webhook/"+id,
		map[string]any{"url": b.URL, "events": b.Events}, ipOf(r))
	writeJSON(w, 201, map[string]any{"id": id, "url": b.URL, "secret": b.Secret, "events": b.Events})
}

func reverseEventName(e string) string {
	for k, v := range eventNames {
		if v == e {
			return k
		}
	}
	return ""
}

// Delete DELETE /{id}
func (s *Service) Delete(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !manageOnly(s.DB, w, r, orgID) {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := s.DB.Exec(`DELETE FROM webhooks WHERE id = ? AND org_id = ?`, id, orgID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, orgID, auth.From(r), "webhook.delete", "org/"+slug+"/webhook/"+id, nil, ipOf(r))
	writeJSON(w, 200, map[string]any{"id": id, "deleted": true})
}

// Deliveries GET /{id}/deliveries?limit=
func (s *Service) Deliveries(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var exists string
	if err := s.DB.QueryRow(`SELECT id FROM webhooks WHERE id = ? AND org_id = ?`, id, orgID).Scan(&exists); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	rows, err := s.DB.Query(`SELECT id, event, status, http_code, attempts, error, created_at, delivered_at
		FROM webhook_deliveries WHERE webhook_id = ? ORDER BY created_at DESC LIMIT ?`, id, limit)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	type d struct {
		ID          string `json:"id"`
		Event       string `json:"event"`
		Status      string `json:"status"`
		HTTPCode    int    `json:"http_code"`
		Attempts    int    `json:"attempts"`
		Error       string `json:"error,omitempty"`
		CreatedAt   string `json:"created_at"`
		DeliveredAt string `json:"delivered_at,omitempty"`
	}
	out := []d{}
	for rows.Next() {
		var x d
		var delAt sql.NullString
		_ = rows.Scan(&x.ID, &x.Event, &x.Status, &x.HTTPCode, &x.Attempts, &x.Error, &x.CreatedAt, &delAt)
		x.DeliveredAt = delAt.String
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"deliveries": out})
}
