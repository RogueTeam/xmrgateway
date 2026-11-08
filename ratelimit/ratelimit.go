// Package ratelimit allows one call per interval for each key.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Limiter[K comparable] struct {
	interval time.Duration

	mu   sync.Mutex
	next map[K]time.Time
}

func New[K comparable](interval time.Duration) *Limiter[K] {
	return &Limiter[K]{interval: interval, next: map[K]time.Time{}}
}

// Allow records a call for key at now. It returns the earliest time the next
// call is allowed and whether this one was.
func (l *Limiter[K]) Allow(key K, now time.Time) (next time.Time, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if next, found := l.next[key]; found && now.Before(next) {
		return next, false
	}
	next = now.Add(l.interval)
	l.next[key] = next
	return next, true
}

// Sweep forgets keys whose wait is over; a missing key is always allowed, so
// memory only grows with the keys used during the last interval.
func (l *Limiter[K]) Sweep(now time.Time) (removed int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, next := range l.next {
		if !now.Before(next) {
			delete(l.next, k)
			removed++
		}
	}
	return removed
}

func (l *Limiter[K]) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.next)
}

// Run sweeps every interval until ctx is done.
func (l *Limiter[K]) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			l.Sweep(now)
		}
	}
}
