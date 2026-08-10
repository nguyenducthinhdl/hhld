package market

import (
	"sync"
)

// Handler receives BookEvents from the Bus.
type Handler func(BookEvent)

// Bus is a bounded in-process pub/sub for BookEvents (miss-more on overflow).
type Bus struct {
	mu      sync.Mutex
	subs    []Handler
	queue   chan BookEvent
	dropped uint64
	closed  bool
}

// NewBus builds a bus with capacity queueSize (drop-newest when full).
func NewBus(queueSize int) *Bus {
	if queueSize <= 0 {
		queueSize = 256
	}
	b := &Bus{queue: make(chan BookEvent, queueSize)}
	go b.dispatch()
	return b
}

// Subscribe registers h. Handlers are invoked serially from the dispatch goroutine.
func (b *Bus) Subscribe(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, h)
}

// Publish enqueues ev. If the queue is full, the event is dropped (miss-more).
func (b *Bus) Publish(ev BookEvent) {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return
	}
	select {
	case b.queue <- ev:
	default:
		b.mu.Lock()
		b.dropped++
		b.mu.Unlock()
	}
}

// Dropped returns how many events were discarded due to a full queue.
func (b *Bus) Dropped() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// Close stops dispatch. Safe to call once.
func (b *Bus) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	close(b.queue)
	b.mu.Unlock()
}

func (b *Bus) dispatch() {
	for ev := range b.queue {
		b.mu.Lock()
		subs := append([]Handler(nil), b.subs...)
		b.mu.Unlock()
		for _, h := range subs {
			h(ev)
		}
	}
}
