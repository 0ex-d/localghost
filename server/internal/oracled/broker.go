package oracled

// Broker ties the queue to the backends. A single worker goroutine pulls the highest-priority,
// non-expired request and runs it on the backend resolved from the request's class. One worker means
// the local model is never asked to do two inferences at once (the hardware reality); the queue in
// front provides fairness, priority, deadlines, and backpressure.
//
// Routing is class -> backend. Today "local-small" resolves to the llama-server child and "frontier"
// resolves to nothing unless the owner configured a frontier backend. A capability ("classify" vs
// "chat") could shape the prompt or pick a different backend later; for now it is passed through and
// the backend treats the Input as the prompt.

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/oracle"
)

// Broker owns the queue, the backends by class, and the worker.
type Broker struct {
	queue    *Queue
	backends map[oracle.Class]Backend
	log      *slog.Logger
	stop     chan struct{}
	wg       sync.WaitGroup
}

// NewBroker builds a broker over a queue of the given depth.
func NewBroker(queueDepth int, log *slog.Logger) *Broker {
	return &Broker{
		queue:    NewQueue(queueDepth),
		backends: map[oracle.Class]Backend{},
		log:      log,
		stop:     make(chan struct{}),
	}
}

// SetBackend registers the backend that serves a class. Called at start once the llama-server child is
// healthy (for local-small) and/or a frontier backend is configured.
func (b *Broker) SetBackend(class oracle.Class, be Backend) {
	b.backends[class] = be
}

// Submit enqueues a request; the caller waits on the returned channel. ok=false means the queue is
// full (backpressure) , the caller should shed.
func (b *Broker) Submit(req oracle.Request) (<-chan oracle.Response, bool) {
	// default class if unset
	if req.Class == "" {
		req.Class = oracle.ClassLocalSmall
	}
	return b.queue.Submit(req)
}

// Run starts the single worker. It blocks until Stop.
func (b *Broker) Run() {
	b.wg.Add(1)
	go b.worker()
}

func (b *Broker) worker() {
	defer b.wg.Done()
	for {
		if !b.queue.Wait(b.stop) {
			return
		}
		// drain everything currently available before waiting again
		for {
			it := b.queue.next()
			if it == nil {
				break
			}
			b.serve(it)
		}
	}
}

func (b *Broker) serve(it *item) {
	be, ok := b.backends[it.req.Class]
	if !ok {
		it.result <- oracle.Response{Err: "no backend for class " + string(it.req.Class)}
		return
	}
	// Bound the inference by the request deadline if set, else a generous ceiling.
	timeout := 120 * time.Second
	if it.req.DeadlineMS > 0 {
		// remaining budget from when it was enqueued
		spent := time.Since(it.enqueued)
		remaining := time.Duration(it.req.DeadlineMS)*time.Millisecond - spent
		if remaining <= 0 {
			it.result <- oracle.Response{Err: "deadline exceeded before dispatch"}
			return
		}
		timeout = remaining
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	resp, err := be.Infer(ctx, it.req)
	if err != nil {
		b.log.Warn("inference failed", "fn", "serve", "capability", it.req.Capability,
			"class", string(it.req.Class), "err", err)
		it.result <- oracle.Response{Err: err.Error()}
		return
	}
	b.log.Debug("inference served", "fn", "serve", "capability", it.req.Capability,
		"model", resp.Model, "ms", time.Since(start).Milliseconds())
	it.result <- resp
}

// Stop halts the worker and drains the queue (failing waiters).
func (b *Broker) Stop() {
	close(b.stop)
	b.queue.Close()
	b.wg.Wait()
}
