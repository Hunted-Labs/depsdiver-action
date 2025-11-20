package worker

import (
	"context"
	"time"
)

type ScheduledJob struct {
	job      Job
	interval time.Duration
	stop     chan struct{}
}

func NewScheduledJob(job Job, interval time.Duration) *ScheduledJob {
	return &ScheduledJob{
		job:      job,
		interval: interval,
		stop:     make(chan struct{}),
	}
}

func (s *ScheduledJob) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			_ = s.job(ctx)
		}
	}
}

func (s *ScheduledJob) Stop() {
	close(s.stop)
}

