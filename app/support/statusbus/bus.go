// Package statusbus is a small in-process pub/sub used to wake up an open
// GET /api/v1/images/{id}/events SSE connection as soon as that request's
// row changes, instead of it having to poll the database itself.
//
// It only signals "something changed" - it never carries the actual status
// payload - so a subscriber always re-reads the current row from the
// database before writing an event. That keeps the database as the single
// source of truth and makes the bus itself trivial: nothing here can go
// stale or get out of order relative to what's actually persisted.
//
// This is in-process only (a Go map guarded by a mutex), which matches how
// this app runs today: the queue workers that process requests run as
// goroutines in the same binary as the HTTP server (see
// bootstrap/queue_runners.go), not as separate processes. If this app is
// ever scaled to multiple server instances behind a load balancer, a
// request's worker and an SSE client watching it can end up on different
// instances, and this bus alone won't bridge that - it would need to be
// backed by something shared (e.g. Redis pub/sub) at that point. Until
// then, this avoids taking on that infrastructure dependency for no
// benefit.
package statusbus

import "sync"

type subscriber struct {
	ch chan struct{}
}

type Bus struct {
	mu   sync.Mutex
	subs map[uint]map[*subscriber]struct{}
}

func New() *Bus {
	return &Bus{subs: make(map[uint]map[*subscriber]struct{})}
}

// Subscribe registers interest in requestID's changes. The returned channel
// receives a value each time Publish(requestID) is called; sends are
// coalesced (buffered 1, non-blocking) so a slow/idle subscriber never
// blocks a publisher and never needs more than one pending wakeup queued -
// missing an intermediate signal is harmless since the subscriber always
// re-reads current state rather than trusting the signal to carry it.
//
// The caller must call the returned unsubscribe func exactly once when it
// stops listening (typically via defer), or this request's subscriber map
// entry leaks for as long as the process runs.
func (b *Bus) Subscribe(requestID uint) (changed <-chan struct{}, unsubscribe func()) {
	sub := &subscriber{ch: make(chan struct{}, 1)}

	b.mu.Lock()
	if b.subs[requestID] == nil {
		b.subs[requestID] = make(map[*subscriber]struct{})
	}
	b.subs[requestID][sub] = struct{}{}
	b.mu.Unlock()

	return sub.ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs[requestID], sub)
		if len(b.subs[requestID]) == 0 {
			delete(b.subs, requestID)
		}
	}
}

// Publish wakes every current subscriber of requestID. Safe to call whether
// or not anything is subscribed (e.g. no one is watching this request yet)
// and from any goroutine (e.g. a queue worker).
func (b *Bus) Publish(requestID uint) {
	b.mu.Lock()
	subs := b.subs[requestID]
	targets := make([]*subscriber, 0, len(subs))
	for s := range subs {
		targets = append(targets, s)
	}
	b.mu.Unlock()

	for _, s := range targets {
		select {
		case s.ch <- struct{}{}:
		default:
			// A signal is already pending for this subscriber - it'll
			// re-read current state when it handles that one, so this
			// Publish needs no separate delivery.
		}
	}
}

// Default is the process-wide bus. Everything in this app shares it - see
// the Subscribe/Publish package funcs below.
var Default = New()

func Subscribe(requestID uint) (changed <-chan struct{}, unsubscribe func()) {
	return Default.Subscribe(requestID)
}

func Publish(requestID uint) {
	Default.Publish(requestID)
}
