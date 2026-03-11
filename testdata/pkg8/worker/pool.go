package worker

import (
	"context"
	"sync"

	"github.com/example/pipeline-runner-dev/pkg/runner"
	_ "github.com/mailru/easyjson" // test: known FOCI package above threshold
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

func (p *Pool) RunPipeline(ctx context.Context, pipeline *runner.Pipeline) error {
	return pipeline.Execute(ctx)
}

func (p *Pool) Stop() {
	close(p.jobs)
	p.wg.Wait()
}

