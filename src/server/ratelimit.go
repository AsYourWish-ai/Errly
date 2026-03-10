package main

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// tokenBucket is a simple per-IP token bucket rate limiter.
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	maxBurst float64
	refillPS float64 // tokens per second
	lastFill time.Time
}

func newBucket(ratePerMin int) *tokenBucket {
	rps := float64(ratePerMin) / 60.0
	return &tokenBucket{
		tokens:   float64(ratePerMin), // start full
		maxBurst: float64(ratePerMin),
		refillPS: rps,
		lastFill: time.Now(),
	}
}

// allow refills the bucket and returns true if a token is available.
func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	b.lastFill = now

	b.tokens += elapsed * b.refillPS
	if b.tokens > b.maxBurst {
		b.tokens = b.maxBurst
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimiter tracks per-IP buckets and evicts stale entries periodically.
type RateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*tokenBucket
	ratePerMin int
	lastPurge  time.Time
}

func NewRateLimiter(ratePerMin int) *RateLimiter {
	return &RateLimiter{
		buckets:    make(map[string]*tokenBucket),
		ratePerMin: ratePerMin,
		lastPurge:  time.Now(),
	}
}

// Allow returns true if the given IP is within its rate limit.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	b, ok := rl.buckets[ip]
	if !ok {
		b = newBucket(rl.ratePerMin)
		rl.buckets[ip] = b
	}
	// Purge buckets that haven't been touched in 5 minutes
	if time.Since(rl.lastPurge) > 5*time.Minute {
		for k, bkt := range rl.buckets {
			bkt.mu.Lock()
			idle := time.Since(bkt.lastFill) > 5*time.Minute
			bkt.mu.Unlock()
			if idle {
				delete(rl.buckets, k)
			}
		}
		rl.lastPurge = time.Now()
	}
	rl.mu.Unlock()
	return b.allow()
}

// clientIP extracts the real client IP from the request, respecting X-Forwarded-For.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain (original client)
		if idx := len(xff); idx > 0 {
			for i, c := range xff {
				if c == ',' {
					xff = xff[:i]
					break
				}
			}
		}
		if ip := net.ParseIP(trimSpace(xff)); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// retryAfterSeconds returns how many seconds until the bucket refills one token.
func retryAfterSeconds(ratePerMin int) string {
	return strconv.Itoa(60 / ratePerMin)
}
