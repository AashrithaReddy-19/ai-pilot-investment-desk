package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"c23desk/internal/model"
)

type fakeQueue struct {
	jobs     []*model.UploadJob
	finished []finish
}

type finish struct {
	id        string
	result    string
	err       string
	permanent bool
	retry     time.Duration
}

func (q *fakeQueue) ClaimUploadJob(context.Context) (*model.UploadJob, error) {
	if len(q.jobs) == 0 {
		return nil, nil
	}
	j := q.jobs[0]
	q.jobs = q.jobs[1:]
	return j, nil
}
func (q *fakeQueue) FinishUploadJob(_ context.Context, id string, result json.RawMessage, jobErr string, permanent bool, retry time.Duration) error {
	q.finished = append(q.finished, finish{id, string(result), jobErr, permanent, retry})
	return nil
}
func (q *fakeQueue) RecoverStuckUploads(context.Context, time.Duration) (int64, error) { return 0, nil }

type fakeEngine struct {
	status int
	body   string
	err    error
	path   string
	sent   string
}

func (e *fakeEngine) DoUpload(_ context.Context, path string, body []byte, ct, _ string) (*model.Upstream, error) {
	e.path, e.sent = path, string(body)
	if e.err != nil {
		return nil, e.err
	}
	return &model.Upstream{Status: e.status, Body: []byte(e.body)}, nil
}

func job(attempts int) *model.UploadJob {
	return &model.UploadJob{ID: "12345678-aaaa", Kind: "balance_sheet", Filename: "bs.csv", ContentType: "text/csv",
		Options: json.RawMessage(`{"unit":"lakh"}`), Content: []byte("Account,Amount\nAI,1\n"), Attempts: attempts, MaxAttempts: 3}
}

func worker(q Queue, e Parser) *Worker {
	return (&Worker{Queue: q, Engine: e, ParsePath: map[string]string{"balance_sheet": "/balance-sheet/parse"},
		Fields: func(o json.RawMessage) map[string]string {
			var m map[string]string
			_ = json.Unmarshal(o, &m)
			return m
		}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Init()
}

func TestWorkerStoresTheParseResult(t *testing.T) {
	q := &fakeQueue{jobs: []*model.UploadJob{job(1)}}
	e := &fakeEngine{status: 200, body: `{"candidates":[]}`}
	w := worker(q, e)
	if !w.Once(context.Background()) || w.Once(context.Background()) {
		t.Fatal("one job, then the queue is empty")
	}
	if q.finished[0].err != "" || q.finished[0].result != `{"candidates":[]}` {
		t.Fatalf("success not recorded: %+v", q.finished)
	}
	if e.path != "/balance-sheet/parse" || !strings.Contains(e.sent, `name="unit"`) || !strings.Contains(e.sent, "AI,1") {
		t.Fatalf("engine got the wrong request: %s %q", e.path, e.sent)
	}
}

func TestWorkerFailsRejectedFilesAtOnceAndRetriesOutages(t *testing.T) {
	q := &fakeQueue{jobs: []*model.UploadJob{job(1)}}
	worker(q, &fakeEngine{status: 422, body: `{"detail":"No numeric amount column found"}`}).Once(context.Background())
	if f := q.finished[0]; !f.permanent || f.err != "No numeric amount column found" {
		t.Fatalf("a rejected file is a permanent failure with the engine's reason: %+v", f)
	}
	q = &fakeQueue{jobs: []*model.UploadJob{job(2)}}
	worker(q, &fakeEngine{err: errors.New("connection refused")}).Once(context.Background())
	if f := q.finished[0]; f.permanent || f.retry != 20*time.Second || !strings.Contains(f.err, "unreachable") {
		t.Fatalf("an outage is retried with backoff: %+v", f)
	}
	q = &fakeQueue{jobs: []*model.UploadJob{job(1)}}
	worker(q, &fakeEngine{status: 503, body: "busy"}).Once(context.Background())
	if f := q.finished[0]; f.permanent {
		t.Fatalf("a 5xx is retried: %+v", f)
	}
}

func TestBackoffGrows(t *testing.T) {
	if Backoff(1) != 5*time.Second || Backoff(2) != 20*time.Second || Backoff(3) != 45*time.Second {
		t.Fatal("backoff is attempts² × 5s")
	}
}

func TestNotifyNeverBlocks(t *testing.T) {
	w := worker(&fakeQueue{}, &fakeEngine{})
	for range 5 {
		w.Notify() // buffered once, extra wakes are dropped
	}
	(&Worker{}).Notify() // not initialised: no panic
}
