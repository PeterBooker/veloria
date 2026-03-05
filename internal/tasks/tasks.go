package tasks

import (
	"context"
	"log"
	"time"

	"github.com/onatm/clockwerk"
)

type Tasks struct {
	c   *clockwerk.Clockwerk
	ctx context.Context
}

type fn func(context.Context) error

type Job struct {
	ctx context.Context
	f   fn
}

func (j *Job) Run() {
	if err := j.f(j.ctx); err != nil {
		log.Printf("scheduled job error: %v", err)
	}
}

func New(ctx context.Context) *Tasks {
	c := clockwerk.New()

	return &Tasks{
		c:   c,
		ctx: ctx,
	}
}

func (t *Tasks) AddJob(f fn, period time.Duration) {
	t.c.Every(period).Do(&Job{
		ctx: t.ctx,
		f:   f,
	})
}

func (t *Tasks) Start() {
	t.c.Start()
}

func (t *Tasks) Stop() {
	t.c.Stop()
}
