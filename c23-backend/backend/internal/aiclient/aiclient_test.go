package aiclient

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

var texts = []Text{{ID: "AI4", Text: "Draft first replies for support agents"}, {ID: "AI5", Text: "Auto-tag support tickets"}}

func server(t *testing.T, h http.HandlerFunc) *Client {
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return New(s.URL, 300*time.Millisecond, quiet)
}

func TestOK(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			return
		}
		_, _ = io.WriteString(w, `{"model_name":"lexical","model_version":"1","advisory":true,"pairs":[{"pilot_a":"AI4","pilot_b":"AI5","semantic_similarity":0.6,"possible_overlap":true,"reason":"shared concept support"}]}`)
	})
	r, st := c.SemanticOverlap(context.Background(), texts)
	if st != StatusOK || len(r.Pairs) != 1 || !r.Pairs[0].PossibleOverlap {
		t.Fatal(st, r)
	}
	if c.Health(context.Background()) != StatusOK {
		t.Fatal("health")
	}
}

func TestFailuresDegradeNeverPanic(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"500":     func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"timeout": func(w http.ResponseWriter, r *http.Request) { time.Sleep(time.Second) },
		"garbage": func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "not json") },
		"bad range": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"model_name":"m","advisory":true,"pairs":[{"pilot_a":"AI4","pilot_b":"AI5","semantic_similarity":7}]}`)
		},
		"hallucinated id": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"model_name":"m","advisory":true,"pairs":[{"pilot_a":"AI4","pilot_b":"AI99","semantic_similarity":0.9}]}`)
		},
		"not advisory": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"model_name":"m","advisory":false,"pairs":[]}`)
		},
	}
	for name, h := range cases {
		c := server(t, h)
		start := time.Now()
		if r, st := c.SemanticOverlap(context.Background(), texts); r != nil || st != StatusDegraded {
			t.Errorf("%s: %v %s", name, r, st)
		}
		if time.Since(start) > 900*time.Millisecond {
			t.Errorf("%s: timeout not enforced", name)
		}
	}
	ung := server(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"model_name":"m","explanation":"AI1 will definitely succeed","grounded":false}`)
	})
	if r, st := ung.Explain(context.Background(), map[string]any{}); r != nil || st != StatusDegraded {
		t.Fatal("ungrounded explanation must be rejected")
	}
}

func TestDisabledAndUnreachable(t *testing.T) {
	var nilClient *Client
	if _, st := nilClient.SemanticOverlap(context.Background(), texts); st != StatusDisabled {
		t.Fatal(st)
	}
	if New("", time.Second, quiet).Health(context.Background()) != StatusDisabled {
		t.Fatal("disabled")
	}
	dead := New("http://127.0.0.1:1", 300*time.Millisecond, quiet)
	if _, st := dead.Explain(context.Background(), nil); st != StatusDegraded || dead.Health(context.Background()) != StatusDegraded {
		t.Fatal("unreachable must degrade")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ok := server(t, func(w http.ResponseWriter, r *http.Request) {})
	if _, st := ok.SemanticOverlap(ctx, texts); st != StatusDegraded {
		t.Fatal("cancelled context must abort the call")
	}
}
