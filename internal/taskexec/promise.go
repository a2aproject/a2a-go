package taskexec

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
)

type promise struct {
	// done channel gets closed once value or err field is set
	done  chan struct{}
	value a2a.SendMessageResult
	err   error
}

func newPromise() *promise {
	return &promise{done: make(chan struct{})}
}

func (p *promise) resolve(value a2a.SendMessageResult) {
	p.value = value
	close(p.done)
}

func (p *promise) reject(err error) {
	p.err = err
	close(p.done)
}

func (r *promise) wait(ctx context.Context) (a2a.SendMessageResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return r.value, r.err
	}
}
