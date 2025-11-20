package messaging

import (
	"context"
	"sync"
)

type Message struct {
	ID      string
	Payload []byte
}

type Queue struct {
	mu       sync.Mutex
	messages []Message
	subs     []chan Message
}

func NewQueue() *Queue {
	return &Queue{
		messages: make([]Message, 0),
		subs:     make([]chan Message, 0),
	}
}

func (q *Queue) Publish(ctx context.Context, msg Message) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.messages = append(q.messages, msg)

	for _, sub := range q.subs {
		select {
		case sub <- msg:
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	return nil
}

func (q *Queue) Subscribe() <-chan Message {
	q.mu.Lock()
	defer q.mu.Unlock()

	ch := make(chan Message, 10)
	q.subs = append(q.subs, ch)
	return ch
}

