// Package jobs runs the upload processing worker: it claims queued uploads, sends the stored file to the
// rules engine parser and records the answer, retrying transient failures with backoff.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"c23desk/internal/model"
	"c23desk/internal/rules"
	"c23desk/internal/storage"
)

// Queue is the store surface the worker needs.
type Queue interface {
	ClaimUploadJob(ctx context.Context) (*model.UploadJob, error)
	FinishUploadJob(ctx context.Context, id string, result json.RawMessage, jobErr string, permanent bool, retryIn time.Duration) error
	RecoverStuckUploads(ctx context.Context, olderThan time.Duration) (int64, error)
}

// Parser sends a multipart body to an engine path.
type Parser interface {
	DoUpload(ctx context.Context, path string, body []byte, contentType, reqID string) (*model.Upstream, error)
}

// Worker processes uploads one at a time. Several gateway instances may each run one: claiming uses
// FOR UPDATE SKIP LOCKED, so no upload is processed twice.
type Worker struct {
	Queue       Queue
	Engine      Parser
	ParsePath   map[string]string                       // kind -> engine parse path
	Fields      func(json.RawMessage) map[string]string // stored options -> engine form fields
	Poll        time.Duration
	CallTimeout time.Duration
	Log         *slog.Logger
	Storage     storage.Store
	wake        chan struct{}
}

// Backoff before attempt n+1 after n failed attempts: 5s, 20s, 45s ...
func Backoff(attempts int) time.Duration { return time.Duration(attempts*attempts) * 5 * time.Second }

// Notify wakes the worker now instead of at the next poll. Safe to call from any goroutine.
func (w *Worker) Notify() {
	if w.wake == nil {
		return
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Init must be called before Run and before Notify is handed out.
func (w *Worker) Init() *Worker {
	w.wake = make(chan struct{}, 1)
	if w.Poll <= 0 {
		w.Poll = time.Second
	}
	if w.CallTimeout <= 0 {
		w.CallTimeout = 30 * time.Second
	}
	return w
}

// Run loops until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	if n, err := w.Queue.RecoverStuckUploads(ctx, 2*time.Minute); err == nil && n > 0 {
		w.Log.Warn("requeued uploads interrupted by a previous shutdown", "count", n)
	}
	t := time.NewTicker(w.Poll)
	defer t.Stop()
	lastRecover := time.Now()
	for {
		for w.Once(ctx) { // drain everything that is due
			if ctx.Err() != nil {
				return
			}
		}
		if time.Since(lastRecover) > time.Minute {
			_, _ = w.Queue.RecoverStuckUploads(ctx, 2*time.Minute)
			lastRecover = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-w.wake:
		}
	}
}

// Once processes at most one due upload and reports whether it found one.
func (w *Worker) Once(ctx context.Context) bool {
	job, err := w.Queue.ClaimUploadJob(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.Log.Error("could not claim an upload job", "err", err)
		}
		return false
	}
	if job == nil {
		return false
	}
	result, jobErr, permanent := w.process(ctx, job)
	retry := Backoff(job.Attempts)
	if err := w.Queue.FinishUploadJob(context.WithoutCancel(ctx), job.ID, result, jobErr, permanent, retry); err != nil {
		w.Log.Error("could not record an upload outcome", "upload_id", job.ID, "err", err)
	}
	switch {
	case jobErr == "":
		w.Log.Info("upload parsed", "upload_id", job.ID, "kind", job.Kind, "attempt", job.Attempts)
	case permanent || job.Attempts >= job.MaxAttempts:
		w.Log.Warn("upload failed", "upload_id", job.ID, "attempt", job.Attempts, "err", jobErr)
	default:
		w.Log.Warn("upload attempt failed, will retry", "upload_id", job.ID, "attempt", job.Attempts, "retry_in", retry, "err", jobErr)
	}
	return true
}

// process returns the engine answer, or an error text and whether retrying could ever help.
func (w *Worker) process(ctx context.Context, job *model.UploadJob) (json.RawMessage, string, bool) {
	path, ok := w.ParsePath[job.Kind]
	if !ok {
		return nil, fmt.Sprintf("unknown upload kind %q", job.Kind), true
	}
	fields := map[string]string{}
	if w.Fields != nil {
		fields = w.Fields(job.Options)
	}
	content := job.Content
	if job.StorageProvider == "s3" {
		if w.Storage == nil || job.ObjectKey == "" {
			return nil, "document storage is not configured", false
		}
		rctx, cancel := context.WithTimeout(ctx, w.CallTimeout)
		defer cancel()
		obj, err := w.Storage.Open(rctx, job.ObjectKey)
		if err != nil {
			return nil, "document storage unavailable: " + err.Error(), false
		}
		content, err = io.ReadAll(io.LimitReader(obj, 2_000_001))
		_ = obj.Close()
		if err != nil || len(content) > 2_000_000 {
			return nil, "stored document exceeds the processing limit", true
		}
	}
	body, ctype, err := rules.Multipart(fields, job.Filename, job.ContentType, content)
	if err != nil {
		return nil, "could not prepare the file for the engine", true
	}
	cctx, cancel := context.WithTimeout(ctx, w.CallTimeout)
	defer cancel()
	r, err := w.Engine.DoUpload(cctx, path, body, ctype, "upload-"+job.ID[:8])
	switch {
	case err != nil:
		return nil, "rules engine unreachable: " + err.Error(), false
	case r.Status >= 500:
		return nil, fmt.Sprintf("rules engine failed (HTTP %d)", r.Status), false
	case r.Status >= 400: // the engine rejected the file itself: retrying the same bytes cannot help
		return nil, engineMessage(r.Body), true
	}
	if !json.Valid(r.Body) {
		return nil, "rules engine returned an unreadable answer", false
	}
	return r.Body, "", false
}

func engineMessage(b []byte) string {
	var d struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(b, &d) == nil && len(d.Detail) > 0 {
		var s string
		if json.Unmarshal(d.Detail, &s) == nil {
			return s
		}
		return string(d.Detail)
	}
	if len(b) > 300 {
		b = b[:300]
	}
	return string(b)
}
