package onlineddl

import (
	"context"
	"sync"
	"time"
)

type controlledProcess struct {
	mu                 sync.Mutex
	process            ToolProcess
	paused, cancelling bool
}
type Controller struct {
	mu     sync.Mutex
	active map[uint64]*controlledProcess
}

func NewController() *Controller { return &Controller{active: map[uint64]*controlledProcess{}} }
func (c *Controller) Register(id uint64, process ToolProcess) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.active[id]; exists {
		return false
	}
	c.active[id] = &controlledProcess{process: process}
	return true
}
func (c *Controller) Unregister(id uint64) { c.mu.Lock(); delete(c.active, id); c.mu.Unlock() }
func (c *Controller) IsCancelling(id uint64) bool {
	c.mu.Lock()
	p := c.active[id]
	c.mu.Unlock()
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancelling
}
func (c *Controller) WithTuning(id uint64, tune func(ToolProcess) error) error {
	c.mu.Lock()
	p := c.active[id]
	c.mu.Unlock()
	if p == nil {
		return ErrInvalidTransition
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelling {
		return ErrInvalidTransition
	}
	return tune(p.process)
}
func (c *Controller) Pause(id uint64) error {
	c.mu.Lock()
	p := c.active[id]
	c.mu.Unlock()
	if p == nil {
		return ErrInvalidTransition
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.paused {
		return nil
	}
	if err := p.process.Pause(); err != nil {
		return err
	}
	p.paused = true
	return nil
}
func (c *Controller) Resume(id uint64) error {
	c.mu.Lock()
	p := c.active[id]
	c.mu.Unlock()
	if p == nil {
		return ErrInvalidTransition
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.paused {
		return nil
	}
	if err := p.process.Resume(); err != nil {
		return err
	}
	p.paused = false
	return nil
}
func (c *Controller) Cancel(ctx context.Context, id uint64, grace time.Duration) error {
	c.mu.Lock()
	p := c.active[id]
	c.mu.Unlock()
	if p == nil {
		return ErrInvalidTransition
	}
	p.mu.Lock()
	if p.cancelling {
		p.mu.Unlock()
		return nil
	}
	p.cancelling = true
	p.mu.Unlock()
	if err := p.process.CancelGracefully(); err != nil {
		p.mu.Lock()
		p.cancelling = false
		p.mu.Unlock()
		return err
	}
	done := make(chan struct{})
	go func() { _, _ = p.process.Wait(); close(done) }()
	if grace <= 0 {
		grace = 5 * time.Second
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		_ = p.process.Terminate()
		return ctx.Err()
	case <-timer.C:
		if err := p.process.Terminate(); err != nil {
			return err
		}
		<-done
		return nil
	}
}
