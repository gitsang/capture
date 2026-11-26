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

// TestParseActressNames covers the javdb 演員 panel markup: every actress
// carries class="actor-female" (all of them in multi-actress works), male
// actors carry no class. The separators are bare commas, so the panel is
// not valid HTML on its own - the parser must still see the anchors.
func TestParseActressNames(t *testing.T) {
	// Trimmed from https://javdb.com/v/82OB6d (PASN-042, 3 actresses) and
	// /v/2mOVQZ (SDJS-390, 4 actresses + 3 actors).
	page := `<html><body>
	<div class="panel-block">
		<strong>演員:</strong>
		&nbsp;<span class="value">
			<a class="actor-female" href="/actors/69A0">河合あすな</a>, <a class="actor-female" href="/actors/4N1Z">野々浦暖</a>, <a class="actor-female" href="/actors/p33Qb">八掛うみ</a>
		</span>
	</div>
	<div class="panel-block">
		<strong>演員:</strong>
		&nbsp;<span class="value">
			<a class="actor-female" href="/actors/xdk6">岩沢香代</a>, <a href="/actors/G955">小田切ジュン</a>, <a href="/actors/EM44">セツネヒデユキ</a>
		</span>
	</div>
	</body></html>`
	names, err := parseActressNames([]byte(page))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, want := range []string{"河合あすな", "野々浦暖", "八掛うみ", "岩沢香代"} {
		if !names[want] {
			t.Errorf("actress %q missing: %v", want, names)
		}
	}
	for _, unwanted := range []string{"小田切ジュン", "セツネヒデユキ"} {
		if names[unwanted] {
			t.Errorf("male actor %q wrongly flagged female", unwanted)
		}
	}
}

// TestParseActressNamesEmpty covers pages with no female-flagged actor.
func TestParseActressNamesEmpty(t *testing.T) {
	names, err := parseActressNames([]byte(`<html><body><p>nothing</p></body></html>`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("want empty set, got %v", names)
	}
}
