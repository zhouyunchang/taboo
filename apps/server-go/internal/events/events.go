// 域事件总线：密钥写路径（创建/更新/回滚/删除）commit 后发布，
// Sync（#11）与 Webhooks（#12）订阅消费。发布非阻塞（异步 goroutine），
// 请求路径只入队，不做外部 HTTP 调用 —— 保证密钥写接口延迟稳定。
package events

import "sync"

// SecretEvent 密钥变更事件（负载不含明文，只有元数据 + 资源定位信息）
type SecretEvent struct {
	EventID     string // 幂等/追踪 ID（投递与同步记录共用）
	OrgID       string
	ProjectID   string
	ProjectSlug string
	EnvSlug     string
	Folder      string
	Key         string
	Version     int
	Action      string // created | updated | rolled_back | deleted
	ActorID     string
	ActorName   string
	ActorKind   string // user | identity
}

// Bus 进程内事件总线（单体架构内足够；水平扩展时替换为队列实现）
type Bus struct {
	mu   sync.RWMutex
	subs []func(SecretEvent)
}

func NewBus() *Bus { return &Bus{} }

func (b *Bus) Subscribe(f func(SecretEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, f)
}

// Publish 异步分发：不阻塞调用方，单订阅者 panic 由 Recoverer 隔离（这里 recover 一层兜底）
func (b *Bus) Publish(e SecretEvent) {
	b.mu.RLock()
	subs := make([]func(SecretEvent), len(b.subs))
	copy(subs, b.subs)
	b.mu.RUnlock()
	for _, f := range subs {
		go func(fn func(SecretEvent)) {
			defer func() { _ = recover() }()
			fn(e)
		}(f)
	}
}
