// 审计落库：返回 error；敏感路径应 fail-closed。
package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/httpx"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

type DBTX interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

type Rec struct {
	OrgID     string
	ActorID   string
	ActorName string
	ActorType string
	Action    string
	Resource  string
	Metadata  map[string]any
	IP        string
	UA        string
	RequestID string
	FailOpen  bool
}

func FromRequest(r *http.Request, orgID, actorID, actorName, actorType, action, resource string, meta map[string]any) Rec {
	return Rec{
		OrgID: orgID, ActorID: actorID, ActorName: actorName, ActorType: actorType,
		Action: action, Resource: resource, Metadata: meta,
		IP: httpx.ClientIP(r), UA: httpx.UserAgent(r), RequestID: httpx.RequestID(r),
	}
}

func Record(db DBTX, rec Rec) error {
	if rec.Metadata == nil {
		rec.Metadata = map[string]any{}
	}
	if rec.ActorID == "" {
		rec.ActorID, rec.ActorName, rec.ActorType = "system", rec.ActorName, "user"
	}
	if rec.ActorType == "" {
		rec.ActorType = "user"
	}
	mb, _ := json.Marshal(rec.Metadata)
	id := tc.NewID()
	created := time.Now().UTC().Format(time.RFC3339Nano)
	prev := lastHash(db, rec.OrgID)
	entry := hashEntry(prev, id, rec.OrgID, rec.ActorID, rec.ActorType, rec.Action, rec.Resource, string(mb), rec.IP, rec.UA, rec.RequestID, created)
	_, err := db.Exec(`INSERT INTO audit_logs
		(id, org_id, actor_id, actor_name, actor_type, action, resource, metadata, ip, user_agent, request_id, prev_hash, entry_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, rec.OrgID, rec.ActorID, rec.ActorName, rec.ActorType, rec.Action, rec.Resource,
		string(mb), rec.IP, rec.UA, rec.RequestID, prev, entry, created)
	if err != nil {
		slog.Error("audit insert failed", "org", rec.OrgID, "action", rec.Action, "err", err)
		if rec.FailOpen {
			return nil
		}
		return err
	}
	return nil
}

func lastHash(db DBTX, orgID string) string {
	var h string
	err := db.QueryRow(`SELECT entry_hash FROM audit_logs WHERE org_id = ? AND entry_hash != ''
		ORDER BY rowid DESC LIMIT 1`, orgID).Scan(&h)
	if err != nil || h == "" {
		return strings.Repeat("0", 64)
	}
	return h
}

func hashEntry(prev, id, orgID, actorID, actorType, action, resource, metadata, ip, ua, rid, created string) string {
	canonical := strings.Join([]string{prev, id, orgID, actorID, actorType, action, resource, metadata, ip, ua, rid, created}, "\n")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

type VerifyResult struct {
	OK         bool   `json:"ok"`
	Checked    int    `json:"checked"`
	BrokenAt   string `json:"broken_at,omitempty"`
	Checkpoint bool   `json:"checkpoint_ok"`
}

func Verify(db DBTX, orgID string, masterKey []byte) (VerifyResult, error) {
	rows, err := db.Query(`SELECT id, actor_id, actor_type, action, resource, metadata, ip,
		COALESCE(user_agent,''), COALESCE(request_id,''), COALESCE(prev_hash,''), COALESCE(entry_hash,''), created_at
		FROM audit_logs WHERE org_id = ? ORDER BY rowid ASC`, orgID)
	if err != nil {
		return VerifyResult{}, err
	}
	defer rows.Close()
	expectPrev := strings.Repeat("0", 64)
	n := 0
	var lastID, lastHash string
	for rows.Next() {
		var id, actorID, actorType, action, resource, metadata, ip, ua, rid, prev, entry, created string
		if err := rows.Scan(&id, &actorID, &actorType, &action, &resource, &metadata, &ip, &ua, &rid, &prev, &entry, &created); err != nil {
			return VerifyResult{}, err
		}
		n++
		if entry == "" {
			expectPrev = strings.Repeat("0", 64)
			continue
		}
		want := hashEntry(expectPrev, id, orgID, actorID, actorType, action, resource, metadata, ip, ua, rid, created)
		if prev != expectPrev || entry != want {
			return VerifyResult{OK: false, Checked: n, BrokenAt: id}, nil
		}
		expectPrev = entry
		lastID, lastHash = id, entry
	}
	cpOK := true
	if lastHash != "" && len(masterKey) > 0 {
		var hid, hhash, macVal string
		err := db.QueryRow(`SELECT last_id, last_hash, hmac FROM audit_checkpoints WHERE org_id = ?
			ORDER BY created_at DESC LIMIT 1`, orgID).Scan(&hid, &hhash, &macVal)
		if err == nil {
			want := checkpointMAC(masterKey, orgID, hid, hhash)
			cpOK = hmac.Equal([]byte(macVal), []byte(want)) && hhash == lastHash && hid == lastID
		}
	}
	return VerifyResult{OK: true, Checked: n, Checkpoint: cpOK}, nil
}

func checkpointMAC(key []byte, orgID, lastID, lastHash string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(orgID + "\n" + lastID + "\n" + lastHash))
	return hex.EncodeToString(mac.Sum(nil))
}

func WriteCheckpoint(db *sql.DB, masterKey []byte, orgID string) error {
	var lastID, lastHash string
	err := db.QueryRow(`SELECT id, entry_hash FROM audit_logs WHERE org_id = ? AND entry_hash != ''
		ORDER BY rowid DESC LIMIT 1`, orgID).Scan(&lastID, &lastHash)
	if err != nil {
		return err
	}
	mac := checkpointMAC(masterKey, orgID, lastID, lastHash)
	_, err = db.Exec(`INSERT INTO audit_checkpoints (id, org_id, last_id, last_hash, hmac, created_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))`, tc.NewID(), orgID, lastID, lastHash, mac)
	return err
}

func StartCheckpointWorker(ctxDone <-chan struct{}, db *sql.DB, masterKey []byte, every time.Duration) {
	if db == nil || len(masterKey) == 0 {
		return
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctxDone:
				return
			case <-t.C:
				rows, err := db.Query(`SELECT DISTINCT org_id FROM audit_logs WHERE entry_hash != ''`)
				if err != nil {
					continue
				}
				var orgs []string
				for rows.Next() {
					var id string
					_ = rows.Scan(&id)
					orgs = append(orgs, id)
				}
				rows.Close()
				for _, o := range orgs {
					_ = WriteCheckpoint(db, masterKey, o)
				}
			}
		}
	}()
}
