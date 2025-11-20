package messaging

import (
	"context"
	"fmt"
	"sync"

	"github.com/example/pipeline-runner-dev/pkg/runner"
)

type Broker struct {
	mu    sync.RWMutex
	queues map[string]*Queue
}

func NewBroker() *Broker {
	return &Broker{
		queues: make(map[string]*Queue),
	}
}

func (b *Broker) GetQueue(name string) *Queue {
	b.mu.Lock()
	defer b.mu.Unlock()

	if queue, exists := b.queues[name]; exists {
		return queue
	}

	queue := NewQueue()
	b.queues[name] = queue
	return queue
}

func (b *Broker) Publish(ctx context.Context, topic string, msg Message) error {
	queue := b.GetQueue(topic)
	return queue.Publish(ctx, msg)
}

func (b *Broker) Subscribe(topic string) (<-chan Message, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	queue, exists := b.queues[topic]
	if !exists {
		return nil, fmt.Errorf("topic %s does not exist", topic)
	}

	return queue.Subscribe(), nil
}

func (b *Broker) ProcessWithPipeline(ctx context.Context, pipeline *runner.Pipeline) error {
	return pipeline.Execute(ctx)
}

