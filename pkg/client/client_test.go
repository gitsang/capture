package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gitsang/capture/pkg/limit"
)

// TestPermanentStatusNotRetried asserts 403 fails immediately (no backoff).
func TestPermanentStatusNotRetried(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := &Client{limiter: limit.New(0), http: srv.Client()}
	start := time.Now()
	_, err := c.fetchBytes(context.Background(), srv.URL+"/x.jpg", 2*time.Second, 5)
	elapsed := time.Since(start)

	var pe *permanentError
	if !errors.As(err, &pe) {
		t.Fatalf("want permanentError, got %v", err)
	}
	if hits != 1 {
		t.Errorf("403 retried %d times, want 1 attempt", hits)
	}
	if elapsed > time.Second {
		t.Errorf("403 should fail fast, took %s", elapsed)
	}
}

// TestTransientStatusRetried asserts 500 is still retried with backoff.
func TestTransientStatusRetried(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := &Client{limiter: limit.New(0), http: srv.Client()}
	data, err := c.fetchBytes(context.Background(), srv.URL+"/x.jpg", time.Millisecond, 5)
	if err != nil {
		t.Fatalf("transient failure: %v", err)
	}
	if string(data) != "ok" || hits != 3 {
		t.Errorf("data=%q hits=%d, want ok/3", data, hits)
	}
}
