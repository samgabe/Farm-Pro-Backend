package api

import (
	"sync"
	"time"
)

type limiterEntry struct {
	count       int
	windowStart time.Time
}

type attemptLimiter struct {
	mu      sync.Mutex
	entries map[string]limiterEntry
	limit   int
	window  time.Duration
}

func newAttemptLimiter(limit int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{
		entries: make(map[string]limiterEntry),
		limit:   limit,
		window:  window,
	}
}

func (l *attemptLimiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.entries[key]
	if !exists || now.Sub(entry.windowStart) >= l.window {
		l.entries[key] = limiterEntry{count: 1, windowStart: now}
		return true
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}
