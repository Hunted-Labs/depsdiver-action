package metrics

import (
	"sync"
	"time"
)

type Counter struct {
	mu    sync.Mutex
	value int64
}

func NewCounter() *Counter {
	return &Counter{}
}

func (c *Counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value++
}

func (c *Counter) Add(delta int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value += delta
}

func (c *Counter) Value() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

type Timer struct {
	mu     sync.Mutex
	values []time.Duration
}

func NewTimer() *Timer {
	return &Timer{
		values: make([]time.Duration, 0),
	}
}

func (t *Timer) Record(duration time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.values = append(t.values, duration)
}

func (t *Timer) Average() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.values) == 0 {
		return 0
	}

	var sum time.Duration
	for _, v := range t.values {
		sum += v
	}

	return sum / time.Duration(len(t.values))
}

