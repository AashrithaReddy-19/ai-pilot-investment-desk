package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"c23desk/internal/config"
	"c23desk/internal/model"
	"c23desk/internal/storage"
)

type objectStorageFake struct {
	body []byte
}

func (f *objectStorageFake) PresignPut(context.Context, string, string, string, int64, time.Duration) (string, map[string]string, error) {
	return "https://s3.example/upload", map[string]string{"Content-Type": "application/pdf"}, nil
}
func (f *objectStorageFake) Head(context.Context, string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{Size: int64(len(f.body)), ContentType: "application/pdf", Metadata: map[string]string{"sha256": checksum(f.body)}}, nil
}
func (f *objectStorageFake) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.body)), nil
}
func (f *objectStorageFake) Delete(context.Context, string) error { return nil }

type objectUploadFake struct {
	*fakeUploads
	upload *model.Upload
}

func (f *objectUploadFake) CreateObjectUpload(_ context.Context, in model.ObjectUploadIn) (*model.Upload, bool, error) {
	f.upload = &model.Upload{ID: in.ID, Filename: in.Filename, ContentType: in.ContentType, Format: in.Format,
		SizeBytes: in.SizeBytes, SHA256: in.SHA256, StorageProvider: "s3", ObjectKey: &in.ObjectKey, Status: "pending_upload"}
	return f.upload, true, nil
}
func (f *objectUploadFake) CompleteObjectUpload(context.Context, string) (*model.Upload, error) {
	f.upload.Status = "received"
	return f.upload, nil
}
func (f *objectUploadFake) GetUpload(context.Context, string) (*model.Upload, error) {
	return f.upload, nil
}

func checksum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestObjectUploadSessionAndCompletion(t *testing.T) {
	body := []byte("%PDF-1.7 test")
	u := &objectUploadFake{fakeUploads: &fakeUploads{}}
	objects := &objectStorageFake{body: body}
	r := NewRouter(Deps{
		Cfg:   config.Config{RequestTimeout: 2 * time.Second, UploadMaxBytes: 4096, PresignedURLExpiry: time.Minute},
		Store: fakeStore{}, Rules: &fakeRules{do: ok200(`{}`)}, Cache: fakeCache{}, Uploads: u,
		ObjectUploads: u, Objects: objects, Log: discardLog(),
	})
	req := `{"filename":"report.pdf","kind":"balance_sheet","size_bytes":` + strconv.Itoa(len(body)) + `,"content_type":"application/pdf","sha256":"` + checksum(body) + `"}`
	w := do(r, http.MethodPost, "/api/v1/documents/upload-session", req)
	if w.Code != http.StatusAccepted || !contains(w.Body.String(), "upload_url") {
		t.Fatalf("session: %d %s", w.Code, w.Body.String())
	}
	w = do(r, http.MethodPost, "/api/v1/documents/"+u.upload.ID+"/complete", "")
	if w.Code != http.StatusOK || u.upload.Status != "received" {
		t.Fatalf("completion: %d %s", w.Code, w.Body.String())
	}
}
