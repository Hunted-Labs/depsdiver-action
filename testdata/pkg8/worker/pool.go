package worker

import (
	"context"
	"sync"
)

type Job func(ctx context.Context) error

type Pool struct {
	workers int
	jobs    chan Job
	wg      sync.WaitGroup
}

func NewPool(workers int) *Pool {
	return &Pool{
		workers: workers,
		jobs:    make(chan Job, workers*2),
	}
}

func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx)
	}
}

func (p *Pool) worker(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-p.jobs:
			if !ok {
				return
			}
			_ = job(ctx)
		}
	}
}

func (p *Pool) Submit(job Job) {
	p.jobs <- job
}

func (p *Pool) Stop() {
	close(p.jobs)
	p.wg.Wait()
}

