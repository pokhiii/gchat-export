// Package ratelimit provides an http.RoundTripper that throttles requests and
// retries transient failures of idempotent requests.
package ratelimit

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/time/rate"
)

const (
	maxAttempts   = 6
	baseDelay     = 500 * time.Millisecond
	maxDelay      = 30 * time.Second
	maxRetryAfter = 60 * time.Second
)

type transport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
	sleep   func(ctx context.Context, d time.Duration) error
	rand    func() float64
}

type Option func(*transport)

// WithSleep replaces the context-aware sleep used between retries.
func WithSleep(f func(ctx context.Context, d time.Duration) error) Option {
	return func(t *transport) { t.sleep = f }
}

// WithRand replaces the jitter source; f returns a value in [0, 1).
func WithRand(f func() float64) Option {
	return func(t *transport) { t.rand = f }
}

// NewTransport wraps base with a token bucket of rps requests per second
// (burst 1) and retries with exponential backoff and full jitter.
func NewTransport(base http.RoundTripper, rps float64, opts ...Option) http.RoundTripper {
	t := &transport{
		base:    base,
		limiter: rate.NewLimiter(rate.Limit(rps), 1),
		sleep:   sleepCtx,
		rand:    rand.Float64,
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	idempotent := req.Method == http.MethodGet || req.Method == http.MethodHead
	for attempt := 0; ; attempt++ {
		if err := t.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		resp, err := t.base.RoundTrip(req)
		if !idempotent || attempt == maxAttempts-1 || !retryable(resp, err) {
			return resp, err
		}
		delay, ok := retryAfter(resp)
		if !ok {
			delay = backoff(attempt, t.rand)
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			_ = resp.Body.Close()
		}
		if err := t.sleep(ctx, delay); err != nil {
			return nil, err
		}
	}
}

func retryable(resp *http.Response, err error) bool {
	if err != nil {
		var ne net.Error
		return (errors.As(err, &ne) && ne.Timeout()) ||
			errors.Is(err, syscall.ECONNRESET) || errors.Is(err, io.ErrUnexpectedEOF)
	}
	switch resp.StatusCode {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

func retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	var d time.Duration
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		d = time.Duration(secs) * time.Second
	} else if at, err := http.ParseTime(v); err == nil {
		d = max(time.Until(at), 0)
	} else {
		return 0, false
	}
	return min(d, maxRetryAfter), true
}

func backoff(attempt int, rnd func() float64) time.Duration {
	d := maxDelay
	if attempt < 16 {
		d = min(maxDelay, baseDelay<<attempt)
	}
	return time.Duration(rnd() * float64(d))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
