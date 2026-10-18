package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"c23desk/internal/docparse"
)

// Balance-sheet documents. POST /balance-sheet/parse and /balance-sheet/confirm take a CSV (passed to the
// engine byte for byte, exactly as before), or a PDF, XLSX or XLS, which this gateway reads into the CSV
// the engine already understands. The engine stays the only place that reads money, applies the units and
// decides which rows look like AI spending; the gateway adds where each row came from and why it was
// flagged. Nothing is stored and no document content is logged. Confirm re-reads the same file the same
// way, so the row numbers the engine returned still point at the same rows.

// statementFields are consumed here and never sent to the engine.
var statementFields = map[string]bool{"sheet": true, "pages": true}

type statementRequest struct {
	body        []byte            // the request exactly as received
	contentType string            // its Content-Type
	file        []byte            // the uploaded document
	fileName    string            // its name, used only to word an error
	fields      map[string]string // every other form field, in order of arrival
	order       []string
}

// readStatement receives the upload. A request that is not a recognisable multipart form is returned
// with file == nil so it can be forwarded unchanged and the engine answers it, as it always did.
func (h *Handlers) readStatement(c *gin.Context) (*statementRequest, bool) {
	docMax := h.cfg.DocumentMaxBytes
	if docMax < h.cfg.MaxBodyBytes {
		docMax = h.cfg.MaxBodyBytes
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, docMax+64<<10) // the document plus form overhead
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			fail(c, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("the upload is larger than the %s limit; split the document or use fewer pages", megabytes(docMax)), "file")
			return nil, false
		}
		fail(c, http.StatusBadRequest, "bad_request", "the upload was interrupted before it finished; try again", "file")
		return nil, false
	}
	req := &statementRequest{body: body, contentType: c.Request.Header.Get("Content-Type"), fields: map[string]string{}}
	mt, params, err := mime.ParseMediaType(req.contentType)
	if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
		return req, true
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fail(c, http.StatusBadRequest, "invalid_multipart", "the upload could not be read as a form; try again", "file")
			return nil, false
		}
		data, err := io.ReadAll(part)
		if err != nil {
			fail(c, http.StatusBadRequest, "invalid_multipart", "the upload could not be read as a form; try again", "file")
			return nil, false
		}
		switch name := part.FormName(); {
		case name == "file" && req.file == nil:
			req.file, req.fileName = data, part.FileName()
		case name != "" && name != "file":
			if _, seen := req.fields[name]; !seen {
				req.order = append(req.order, name)
			}
			req.fields[name] = string(data)
		}
	}
	if req.file == nil {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "send the document as the multipart field \"file\"", "file")
		return nil, false
	}
	return req, true
}

func megabytes(n int64) string {
	if n >= 1<<20 {
		return strconv.FormatInt(n>>20, 10) + " MB"
	}
	return strconv.FormatInt(n, 10) + " bytes"
}

// docStatus maps a refusal to an HTTP status.
func docStatus(code string) int {
	switch code {
	case docparse.CodeTooLarge:
		return http.StatusRequestEntityTooLarge
	case docparse.CodeTimeout:
		return http.StatusGatewayTimeout
	}
	return http.StatusUnprocessableEntity
}

// statementUpload serves /balance-sheet/parse and /balance-sheet/confirm.
func (h *Handlers) statementUpload(enginePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// A document can take longer to receive and read than an ordinary request. Only this route gets the
		// longer read/write deadline and a request context that is not cut at the usual few seconds.
		timeout := h.cfg.DocumentTimeout
		if timeout <= 0 {
			timeout = h.cfg.RequestTimeout
		}
		rc := http.NewResponseController(c.Writer)
		_ = rc.SetReadDeadline(time.Now().Add(timeout))
		_ = rc.SetWriteDeadline(time.Now().Add(timeout + 10*time.Second))

		req, ok := h.readStatement(c)
		if !ok {
			return
		}
		// The global request deadline is only a few seconds and a document can take longer than that to
		// arrive, so everything after the upload (reading it, the engine call) gets its own budget that
		// starts now and is not tied to the request's expired context.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), timeout)
		defer cancel()
		if req.file == nil { // not a form we recognise: the engine answers it, as before
			if int64(len(req.body)) > h.cfg.MaxBodyBytes {
				fail(c, http.StatusRequestEntityTooLarge, "payload_too_large", fmt.Sprintf("request body exceeds %d bytes", h.cfg.MaxBodyBytes), "")
				return
			}
			h.forwardStatement(ctx, c, enginePath, req.body, req.contentType, nil, nil)
			return
		}
		format, err := docparse.Detect(req.fileName, req.file)
		if err != nil {
			h.statementError(c, err)
			return
		}
		if format == docparse.FormatCSV {
			if int64(len(req.body)) > h.cfg.MaxBodyBytes {
				fail(c, http.StatusRequestEntityTooLarge, "payload_too_large", fmt.Sprintf("request body exceeds %d bytes", h.cfg.MaxBodyBytes), "")
				return
			}
			h.forwardStatement(ctx, c, enginePath, req.body, req.contentType, nil, req.file)
			return
		}

		// PDF, XLSX or XLS: read it into CSV. Reading is CPU work, so only a few run at a time.
		if h.docSlots != nil {
			select {
			case h.docSlots <- struct{}{}:
				defer func() { <-h.docSlots }()
			default:
				c.Header("Retry-After", "5")
				fail(c, http.StatusTooManyRequests, "busy", "other documents are being read right now; try again in a few seconds", "")
				return
			}
		}
		opt := docparse.Options{Sheet: req.fields["sheet"], Pages: req.fields["pages"], MaxPages: h.cfg.DocumentMaxPages}
		start := time.Now()
		res, err := h.convertDocument(ctx, format, req.file, opt)
		if err != nil {
			h.log.Info("document not converted", "format", format, "bytes", len(req.file), "ms", time.Since(start).Milliseconds(), "request_id", reqID(c))
			h.statementError(c, err)
			return
		}
		h.log.Info("document converted", "format", format, "bytes", len(req.file), "rows", res.Rows, "ms", time.Since(start).Milliseconds(), "request_id", reqID(c))

		body, ct, err := rebuildStatement(req, res.CSV)
		if err != nil {
			fail(c, http.StatusInternalServerError, "internal_error", "the document could not be prepared for the rules engine", "")
			return
		}
		h.forwardStatement(ctx, c, enginePath, body, ct, res, nil)
	}
}

// convertDocument runs the conversion where it can be abandoned: the reader checks the context between
// pages and sheets, and a panic inside a parsing library becomes an ordinary refusal.
func (h *Handlers) convertDocument(ctx context.Context, format string, file []byte, opt docparse.Options) (*docparse.Result, error) {
	type out struct {
		res *docparse.Result
		err error
	}
	ch := make(chan out, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- out{err: &docparse.Error{Code: docparse.CodeCorrupt, Field: "file", Message: "the document could not be read (it may be damaged); open it in its application and save it again"}}
			}
		}()
		res, err := docparse.Convert(ctx, format, file, opt)
		ch <- out{res, err}
	}()
	select {
	case o := <-ch:
		return o.res, o.err
	case <-ctx.Done():
		return nil, &docparse.Error{Code: docparse.CodeTimeout, Message: "reading the document took too long; choose fewer pages or a smaller file"}
	}
}

func (h *Handlers) statementError(c *gin.Context, err error) {
	var de *docparse.Error
	if errors.As(err, &de) {
		fail(c, docStatus(de.Code), de.Code, de.Message, de.Field)
		return
	}
	h.log.Error("document conversion failed", "err", err, "request_id", reqID(c))
	fail(c, http.StatusInternalServerError, "internal_error", "the document could not be read", "file")
}

// rebuildStatement is the original form with the document replaced by its CSV and the gateway-only fields
// (sheet, pages) removed. Every other field reaches the engine as the person sent it.
func rebuildStatement(req *statementRequest, csvBytes []byte) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, name := range req.order {
		if statementFields[name] {
			continue
		}
		if err := w.WriteField(name, req.fields[name]); err != nil {
			return nil, "", err
		}
	}
	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{`form-data; name="file"; filename="statement.csv"`}
	hdr["Content-Type"] = []string{"text/csv"}
	fw, err := w.CreatePart(hdr)
	if err != nil {
		return nil, "", err
	}
	if _, err := fw.Write(csvBytes); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// forwardStatement sends the body to the engine and returns its answer, adding what the gateway knows.
func (h *Handlers) forwardStatement(ctx context.Context, c *gin.Context, enginePath string, body []byte, contentType string, res *docparse.Result, csvFile []byte) {
	if contentType == "" {
		contentType = "multipart/form-data"
	}
	r, err := h.rules.DoUpload(ctx, enginePath, body, contentType, reqID(c))
	if err == nil && r != nil && r.Status >= 500 && csvFile != nil {
		// The engine's balance-sheet reader fails with a bare server error on a row with more cells than the
		// header (for example an unquoted 1,20,000). Say what is wrong instead of "rules engine failed".
		if why := diagnoseCSV(csvFile); why != "" {
			h.log.Warn("rules engine failed on a CSV with rows wider than its header", "request_id", reqID(c))
			fail(c, http.StatusUnprocessableEntity, "csv_row_too_wide", why, "file")
			return
		}
	}
	if !h.upstreamOK(c, r, err) {
		return
	}
	var set map[string]string
	if sheetOrPages := fieldsOf(body, contentType); sheetOrPages != nil {
		set = sheetOrPages
	}
	out := augmentStatement(r.Body, res, set)
	c.Data(http.StatusOK, "application/json", out)
}

// fieldsOf reads the plain form fields of a multipart body (used to learn what the person chose).
func fieldsOf(body []byte, contentType string) map[string]string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return nil
	}
	out := map[string]string{}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		p, err := mr.NextPart()
		if err != nil {
			return out
		}
		if p.FormName() != "file" && p.FormName() != "" {
			b, _ := io.ReadAll(io.LimitReader(p, 1<<16))
			out[p.FormName()] = string(b)
		}
	}
}

// diagnoseCSV finds the first row with more cells than the header and says so. It returns "" when no row
// is wider (so the failure has some other cause).
func diagnoseCSV(data []byte) string {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	first := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		first = data[:i]
	}
	delim, best := ',', -1
	for _, d := range []rune{',', ';', '\t', '|'} {
		if n := bytes.Count(first, []byte(string(d))); n > best {
			delim, best = d, n
		}
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma, r.LazyQuotes, r.FieldsPerRecord = delim, true, -1
	header, err := r.Read()
	if err != nil {
		return ""
	}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return ""
		}
		if err != nil {
			continue
		}
		line, _ := r.FieldPos(0) // the line the record just read starts on
		if len(rec) > len(header) {
			return fmt.Sprintf("Row %d has %d cells but the header row has %d. A number written with commas, such as 1,20,000, must be wrapped in quotes (\"1,20,000\"), and notes or merged cells beside the amounts should be removed. Fix that row and upload again.", line, len(rec), len(header))
		}
	}
}

// engineDetail turns the engine's {"detail": ...} error body into a sentence.
func engineDetail(body []byte) string {
	var d struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(body, &d) != nil || len(d.Detail) == 0 {
		return string(body)
	}
	var s string
	if json.Unmarshal(d.Detail, &s) == nil {
		return s
	}
	var list []struct {
		Loc []any  `json:"loc"`
		Msg string `json:"msg"`
	}
	if json.Unmarshal(d.Detail, &list) == nil && len(list) > 0 {
		parts := make([]string, 0, len(list))
		for _, e := range list {
			field := ""
			if n := len(e.Loc); n > 0 {
				field = fmt.Sprint(e.Loc[n-1])
			}
			if field != "" && field != "body" {
				parts = append(parts, field+": "+e.Msg)
			} else {
				parts = append(parts, e.Msg)
			}
		}
		return strings.Join(parts, "; ")
	}
	return string(body)
}
