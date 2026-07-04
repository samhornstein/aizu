// Package ratelimit provides a token-bucket rate limiter for controlling
// the rate at which Aizu enqueues and processes tasks.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter implements a token-bucket rate limiter.
// When capacity is 0, the limiter is disabled and always allows requests.
type Limiter struct {
	mu         sync.Mutex
	capacity   int // max tokens (requests allowed per window)
	tokens     int
	window     time.Duration
	refillTime time.Time
}

// New creates a new rate limiter.
// A capacity of 0 creates a disabled limiter that always allows requests.
func New(capacity int, window time.Duration) *Limiter {
	l := &Limiter{
		capacity: capacity,
		tokens:   capacity,
		window:   window,
	}
	if capacity > 0 {
		l.refillTime = time.Now()
	}
	return l
}

// Allow checks if a request is allowed under the current rate limit.
// Returns true if the request is allowed, false if it would exceed the limit.
func (l *Limiter) Allow() bool {
	if l.capacity == 0 {
		return true // disabled
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// Refill tokens based on elapsed time.
	if now.Sub(l.refillTime) >= l.window {
		l.tokens = l.capacity
		l.refillTime = now
	}

	if l.tokens > 0 {
		l.tokens--
		return true
	}
	return false
}

// Wait blocks until a token is available or ctx is cancelled.
// Returns an error only if ctx is cancelled.
func (l *Limiter) Wait(ctx waitContext) error {
	if l.capacity == 0 {
		return nil // disabled
	}

	for {
		if l.Allow() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// retry
		}
	}
}

// waitContext is the minimal context interface we need.
type waitContext interface {
	Done() <-chan struct{}
	Err() error
}
