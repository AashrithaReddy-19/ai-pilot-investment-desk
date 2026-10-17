package rules

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoForwardsBodyAndRequestID(t *testing.T) {
	var gotBody, gotID, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotID, gotCT = string(b), r.Header.Get("X-Request-ID"), r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(srv.URL, 2*time.Second)
	res, err := c.Do(context.Background(), http.MethodPost, "/portfolio", []byte(`{"budget":600000}`), "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 || string(res.Body) != `{"ok":true}` {
		t.Fatalf("unexpected response: %+v", res)
	}
	if gotBody != `{"budget":600000}` || gotID != "req-1" || gotCT != "application/json" {
		t.Fatalf("request not forwarded correctly: body=%q id=%q ct=%q", gotBody, gotID, gotCT)
	}
}

func TestDoReturnsNon2xxAsResponseNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":"mode must be independent or conservative"}`))
	}))
	defer srv.Close()
	res, err := New(srv.URL, time.Second).Do(context.Background(), http.MethodPost, "/portfolio", []byte(`{}`), "")
	if err != nil || res.Status != 422 {
		t.Fatalf("want 422 response without error, got %+v, %v", res, err)
	}
}

func TestDoTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	_, err := New(srv.URL, 50*time.Millisecond).Do(context.Background(), http.MethodGet, "/slow", nil, "")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
}

func TestPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	if err := New(srv.URL, time.Second).Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := New("http://127.0.0.1:1", 200*time.Millisecond).Ping(context.Background()); err == nil {
		t.Fatal("expected ping to fail for an unreachable engine")
	}
}
