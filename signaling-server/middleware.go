package main

import (
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

func newIPLimiter() *ipLimiter {
	l := &ipLimiter{limiters: make(map[string]*rate.Limiter)}
	go l.cleanup()
	return l
}

// NOTE. cleanup prevents the map from growing unbounded from one-off IPs.
func (l *ipLimiter) cleanup() {
	for range time.Tick(10 * time.Minute) {
		l.mu.Lock()
		l.limiters = make(map[string]*rate.Limiter)
		l.mu.Unlock()
	}
}

// NOTE. getLimiter returns (creating if needed) a limiter allowing ~5 requests 
// per minute with a burst of 5, per IP.
func (l *ipLimiter) getLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim, ok := l.limiters[ip]
	if !ok {
		lim = rate.NewLimiter(rate.Every(12*time.Second), 5)
		l.limiters[ip] = lim
	}
	return lim
}

func clientIP(r *http.Request) string {
	// Trust X-Forwarded-For only because Caddy sits in front and sets it
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil { return r.RemoteAddr }
	return host
}


var authLimiter = newIPLimiter()


func rateLimited(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !authLimiter.getLimiter(ip).Allow() {
			writeError(w, http.StatusTooManyRequests, "too many attempts, slow down")
			return
		}
		h(w, r)
	}
}