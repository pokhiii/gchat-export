package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type recorder struct{ slept []time.Duration }

func (r *recorder) sleep(ctx context.Context, d time.Duration) error {
	r.slept = append(r.slept, d)
	return ctx.Err()
}

// server replies with statuses in order, repeating the last one.
func server(t *testing.T, statuses []int, header http.Header) (*httptest.Server, *int32) {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(atomic.AddInt32(&n, 1)) - 1
		if i >= len(statuses) {
			i = len(statuses) - 1
		}
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(statuses[i])
		w.Write([]byte("body"))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func client(rec *recorder, rnd float64) *http.Client {
	return &http.Client{Transport: NewTransport(http.DefaultTransport, 1000,
		WithSleep(rec.sleep), WithRand(func() float64 { return rnd }))}
}

func do(t *testing.T, c *http.Client, method, url string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(""))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestRetriesOn503ThenSucceeds(t *testing.T) {
	srv, n := server(t, []int{503, 503, 200}, nil)
	resp := do(t, client(&recorder{}, 1), "GET", srv.URL)
	if resp.StatusCode != 200 || *n != 3 {
		t.Fatalf("status %d after %d requests", resp.StatusCode, *n)
	}
}

func TestGivesUpAfter6Attempts(t *testing.T) {
	srv, n := server(t, []int{500}, nil)
	resp := do(t, client(&recorder{}, 1), "GET", srv.URL)
	if resp.StatusCode != 500 || *n != 6 {
		t.Fatalf("status %d after %d requests", resp.StatusCode, *n)
	}
}

func TestNoRetryOn400And403(t *testing.T) {
	for _, code := range []int{400, 403} {
		srv, n := server(t, []int{code}, nil)
		do(t, client(&recorder{}, 1), "GET", srv.URL)
		if *n != 1 {
			t.Errorf("%d: %d requests", code, *n)
		}
	}
}

func TestNoRetryOnPOST(t *testing.T) {
	srv, n := server(t, []int{503}, nil)
	do(t, client(&recorder{}, 1), "POST", srv.URL)
	if *n != 1 {
		t.Fatalf("%d requests", *n)
	}
}

func TestHonorsRetryAfterSeconds(t *testing.T) {
	srv, _ := server(t, []int{429, 200}, http.Header{"Retry-After": {"7"}})
	rec := &recorder{}
	do(t, client(rec, 1), "GET", srv.URL)
	if len(rec.slept) != 1 || rec.slept[0] != 7*time.Second {
		t.Fatalf("slept %v", rec.slept)
	}
}

func TestRetryAfterCapped(t *testing.T) {
	srv, _ := server(t, []int{429, 200}, http.Header{"Retry-After": {"3600"}})
	rec := &recorder{}
	do(t, client(rec, 1), "GET", srv.URL)
	if len(rec.slept) != 1 || rec.slept[0] != 60*time.Second {
		t.Fatalf("slept %v", rec.slept)
	}
}

func TestBackoffFullJitterBounds(t *testing.T) {
	srv, _ := server(t, []int{500}, nil)
	rec := &recorder{}
	do(t, client(rec, 1), "GET", srv.URL)
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	if len(rec.slept) != len(want) {
		t.Fatalf("slept %v", rec.slept)
	}
	for i := range want {
		if rec.slept[i] != want[i] {
			t.Fatalf("slept %v, want %v", rec.slept, want)
		}
	}
	rec = &recorder{}
	do(t, client(rec, 0), "GET", srv.URL)
	for _, d := range rec.slept {
		if d != 0 {
			t.Fatalf("rand=0 slept %v", rec.slept)
		}
	}
}

func TestBackoffCappedAt30s(t *testing.T) {
	if d := backoff(10, func() float64 { return 1 }); d != 30*time.Second {
		t.Fatalf("backoff(10) = %v", d)
	}
}

func TestContextCancelStopsRetry(t *testing.T) {
	srv, n := server(t, []int{503}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	c := &http.Client{Transport: NewTransport(http.DefaultTransport, 1000,
		WithSleep(func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }))}
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	_, err := c.Do(req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if *n != 1 {
		t.Fatalf("%d requests", *n)
	}
}

func TestRateLimitApplied(t *testing.T) {
	srv, _ := server(t, []int{200}, nil)
	c := &http.Client{Transport: NewTransport(http.DefaultTransport, 2)}
	start := time.Now()
	for i := 0; i < 3; i++ {
		do(t, c, "GET", srv.URL)
	}
	if el := time.Since(start); el < 900*time.Millisecond {
		t.Fatalf("3 requests at 2 rps took %v", el)
	}
}
