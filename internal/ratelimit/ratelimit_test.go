package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestDisabledLimiter(t *testing.T) {
	l := New(0, time.Minute)
	if !l.Allow() {
		t.Error("disabled limiter should always allow")
	}
	if !l.Allow() {
		t.Error("disabled limiter should always allow (second call)")
	}
}

func TestLimiterAllowsUpToCapacity(t *testing.T) {
	l := New(3, time.Minute)

	for i := 0; i < 3; i++ {
		if !l.Allow() {
			t.Errorf("Allow() should return true for request %d", i+1)
		}
	}

	if l.Allow() {
		t.Error("Allow() should return false after capacity is exhausted")
	}
}

func TestLimiterRefillsAfterWindow(t *testing.T) {
	l := New(2, 50*time.Millisecond)

	// Exhaust tokens
	if !l.Allow() {
		t.Error("first allow should succeed")
	}
	if !l.Allow() {
		t.Error("second allow should succeed")
	}
	if l.Allow() {
		t.Error("third allow should fail")
	}

	// Wait for window to pass
	time.Sleep(60 * time.Millisecond)

	// Should have refilled
	if !l.Allow() {
		t.Error("allow should succeed after window refill")
	}
	if !l.Allow() {
		t.Error("second allow should succeed after window refill")
	}
	if l.Allow() {
		t.Error("third allow should fail after refill")
	}
}

func TestWaitReturnsImmediatelyWhenAvailable(t *testing.T) {
	l := New(1, time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := l.Wait(ctx)
	if err != nil {
		t.Errorf("Wait() should return nil when token is available, got %v", err)
	}
}

func TestWaitBlocksWhenExhausted(t *testing.T) {
	l := New(1, 50*time.Millisecond)

	// Exhaust the token
	if !l.Allow() {
		t.Fatal("first allow should succeed")
	}

	// Start waiting in a goroutine
	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		l.Wait(ctx)
		close(done)
	}()

	// Should not return immediately
	select {
	case <-done:
		t.Error("Wait() should have blocked when tokens were exhausted")
	case <-time.After(10 * time.Millisecond):
		// expected - it's blocking
	}

	// Wait for it to eventually succeed after refill
	select {
	case <-done:
		// success
	case <-time.After(500 * time.Millisecond):
		t.Error("Wait() should have returned after refill")
	}
}

func TestWaitReturnsOnContextCancel(t *testing.T) {
	l := New(1, time.Hour) // long window so tokens don't refill

	// Exhaust the token
	l.Allow()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := l.Wait(ctx)
	if err == nil {
		t.Error("Wait() should return error when context is cancelled")
	}
}

func TestConcurrentAccess(t *testing.T) {
	l := New(100, time.Minute)

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- l.Allow()
		}()
	}

	wg.Wait()
	close(allowed)

	count := 0
	for ok := range allowed {
		if ok {
			count++
		}
	}

	if count != 100 {
		t.Errorf("expected exactly 100 allowed requests, got %d", count)
	}
}
