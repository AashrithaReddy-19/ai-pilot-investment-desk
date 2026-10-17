// Package testutil gives tests access to the immutable official data in decision-engine/data/raw.
package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/fixture"
	"c23desk/internal/rules"
)

// RawDir returns the absolute path of decision-engine/data/raw (or $C23_RAW_DIR).
func RawDir() string {
	if d := os.Getenv("C23_RAW_DIR"); d != "" {
		return d
	}
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "decision-engine", "data", "raw")
}

// Official loads the verified official fixture + team extension.
func Official(t testing.TB) fixture.Result {
	t.Helper()
	r, err := fixture.LoadOfficial(RawDir())
	if err != nil {
		t.Fatalf("load official: %v", err)
	}
	return r
}

// ByID indexes pilots.
func ByID(ps []domain.Pilot) map[string]domain.Pilot {
	m := map[string]domain.Pilot{}
	for _, p := range ps {
		m[p.ID] = p
	}
	return m
}

// EvaluateAll evaluates pilots with group-peer context, as the service does.
func EvaluateAll(ps []domain.Pilot) map[string]decision.Evaluation {
	out := map[string]decision.Evaluation{}
	for _, p := range ps {
		out[p.ID] = decision.Evaluate(p, rules.Default(), decision.Context{GroupPeers: Peers(ps, p)})
	}
	return out
}

// Peers lists other pilots sharing p's known overlap group.
func Peers(ps []domain.Pilot, p domain.Pilot) []string {
	var out []string
	if p.OverlapGroup == nil {
		return out
	}
	for _, q := range ps {
		if q.ID != p.ID && q.OverlapGroup != nil && *q.OverlapGroup == *p.OverlapGroup {
			out = append(out, q.ID)
		}
	}
	return out
}
