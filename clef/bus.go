package clef

import "sync"

// Subscriber 是 Bus 的订阅方接口。
type Subscriber interface {
	Match(e *Event) bool
	Deliver(e *Event)
}

// SubscriberFunc 是基于函数字面量的便捷实现。
type SubscriberFunc struct {
	MatchFn   func(*Event) bool
	DeliverFn func(*Event)
}

func (s SubscriberFunc) Match(e *Event) bool { return s.MatchFn(e) }
func (s SubscriberFunc) Deliver(e *Event)   { s.DeliverFn(e) }

// SubscriptionHandle 由 Subscribe 返回，调用 Unsubscribe 停止派发。
type SubscriptionHandle struct {
	bus *Bus
	id  uint64
}

func (h SubscriptionHandle) Unsubscribe() {
	if h.bus != nil {
		h.bus.unsubscribe(h.id)
	}
}

// Bus 是 lossy 内存事件总线。Publish 永远不阻塞：当订阅方处理慢、buffer 满时
// 新事件被丢弃。需要持久化的订阅方应调高 perSubBuffer 或在订阅方内部走非
// lossy 队列。
type Bus struct {
	mu           sync.Mutex
	subs         map[uint64]*subscription
	nextID       uint64
	closed       bool
	perSubBuffer int
}

type subscription struct {
	sub  Subscriber
	ch   chan *Event
	done chan struct{}
}

// NewBus 创建总线；perSubBuffer 是每个订阅方的内部 channel 容量，<= 0 时取 64。
func NewBus(perSubBuffer int) *Bus {
	if perSubBuffer <= 0 {
		perSubBuffer = 64
	}
	return &Bus{subs: map[uint64]*subscription{}, perSubBuffer: perSubBuffer}
}

// Subscribe 注册一个订阅方；返回 handle 用于撤销。
func (b *Bus) Subscribe(s Subscriber) SubscriptionHandle {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return SubscriptionHandle{}
	}
	b.nextID++
	id := b.nextID
	sub := &subscription{
		sub:  s,
		ch:   make(chan *Event, b.perSubBuffer),
		done: make(chan struct{}),
	}
	b.subs[id] = sub
	go b.run(sub)
	return SubscriptionHandle{bus: b, id: id}
}

func (b *Bus) unsubscribe(id uint64) {
	b.mu.Lock()
	sub, ok := b.subs[id]
	if !ok {
		b.mu.Unlock()
		return
	}
	delete(b.subs, id)
	b.mu.Unlock()
	close(sub.ch) // signal run() to drain and exit
	<-sub.done    // wait for run() to finish
}

// Publish 投递事件给所有匹配的订阅方；不阻塞订阅方处理（select default 丢弃满 channel 的事件）。
// 持锁期间执行非阻塞发送，与 Close/unsubscribe 互斥，避免与关闭操作竞争。
func (b *Bus) Publish(e *Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for _, s := range b.subs {
		if !s.sub.Match(e) {
			continue
		}
		select {
		case s.ch <- e:
		default:
			// buffer 满 → 丢弃（lossy 设计）
		}
	}
}

// Close 关闭所有订阅方；先关闭它们的输入 channel 让 run() 把已入队事件处理完，
// 再等待所有 run goroutine 退出。返回后任何 Publish/Subscribe 都是 no-op。
func (b *Bus) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	subs := make([]*subscription, 0, len(b.subs))
	for id, s := range b.subs {
		subs = append(subs, s)
		delete(b.subs, id)
	}
	b.mu.Unlock()
	// Close subscriber input channels: run() will drain remaining events then exit.
	for _, s := range subs {
		close(s.ch)
	}
	// Wait for run goroutines to finish draining.
	for _, s := range subs {
		<-s.done
	}
}

func (b *Bus) run(s *subscription) {
	defer close(s.done)
	for e := range s.ch {
		s.sub.Deliver(e)
	}
}
