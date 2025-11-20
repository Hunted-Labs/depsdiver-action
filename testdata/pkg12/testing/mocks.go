package testing

import (
	"context"
	"time"
)

type MockClock struct {
	now time.Time
}

func NewMockClock(initialTime time.Time) *MockClock {
	return &MockClock{now: initialTime}
}

func (m *MockClock) Now() time.Time {
	return m.now
}

func (m *MockClock) Advance(duration time.Duration) {
	m.now = m.now.Add(duration)
}

type MockContext struct {
	context.Context
	deadline time.Time
	done     chan struct{}
}

func NewMockContext() *MockContext {
	return &MockContext{
		Context: context.Background(),
		done:    make(chan struct{}),
	}
}

func (m *MockContext) Done() <-chan struct{} {
	return m.done
}

func (m *MockContext) Cancel() {
	close(m.done)
}

