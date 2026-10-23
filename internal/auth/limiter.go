package auth

import (
	"sync"
	"time"
)

// Limiter throttles failed login attempts. The app may be reachable from the
// internet through a port forward, so a handful of wrong passwords must not
// turn into an unlimited guessing loop.
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu    sync.Mutex
	fails map[string][]time.Time
}

// NewLimiter allows max failures per key (usually IP + username) inside window;
// further attempts are refused until the oldest failure ages out.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{
		max:    max,
		window: window,
		now:    time.Now,
		fails:  make(map[string][]time.Time),
	}
}

// RetryAfter reports how long the caller has to wait before another attempt is
// allowed. Zero means "go ahead".
func (l *Limiter) RetryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	times := l.recentLocked(key)
	if len(times) < l.max {
		return 0
	}
	oldest := times[len(times)-l.max]
	wait := l.window - l.now().Sub(oldest)
	if wait < 0 {
		wait = 0
	}
	return wait
}

// Fail records a failed attempt.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[key] = append(l.recentLocked(key), l.now())
}

// Reset clears the counters for a key after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

// recentLocked keeps only the failures inside the window. Callers must hold l.mu.
func (l *Limiter) recentLocked(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	times := l.fails[key][:0]
	for _, t := range l.fails[key] {
		if t.After(cutoff) {
			times = append(times, t)
		}
	}
	if len(times) == 0 {
		delete(l.fails, key)
		return nil
	}
	l.fails[key] = times
	return times
}
