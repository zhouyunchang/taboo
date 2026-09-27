// Durable fan-out: outbox acknowledgment and downstream task creation share a transaction.
package events

import (
	"database/sql"
	"fmt"
	"sync"
)

// SecretEvent 密钥变更事件（负载不含明文，只有元数据 + 资源定位信息）
type SecretEvent struct {
	SecretID      string
	EnvironmentID string
	Ciphertext    string // Encrypted immutable snapshot for delayed sync; never sent to webhook recipients.
	EventID       string // 幂等/追踪 ID（投递与同步记录共用）
	OrgID         string
	ProjectID     string
	ProjectSlug   string
	EnvSlug       string
	Folder        string
	Key           string
	Version       int
	Action        string // created | updated | rolled_back | deleted
	ActorID       string
	ActorName     string
	ActorKind     string // user | identity
}

// Bus 进程内事件总线（单体架构内足够；水平扩展时替换为队列实现）
type Bus struct {
	mu   sync.RWMutex
	subs []func(*sql.Tx, SecretEvent) error
}

func NewBus() *Bus { return &Bus{} }

// Subscribe registers a durable task materializer. It must only use tx and
// must not contact downstream services. Any error rolls back the whole fan-out.
func (b *Bus) Subscribe(f func(*sql.Tx, SecretEvent) error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, f)
}
func (b *Bus) Dispatch(tx *sql.Tx, e SecretEvent) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("durable event consumer panicked")
		}
	}()
	b.mu.RLock()
	subs := append([]func(*sql.Tx, SecretEvent) error(nil), b.subs...)
	b.mu.RUnlock()
	if len(subs) == 0 {
		return fmt.Errorf("no durable event consumers registered")
	}
	for _, f := range subs {
		if err = f(tx, e); err != nil {
			return err
		}
	}
	return nil
}
