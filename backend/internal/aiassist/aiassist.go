// Package aiassist is the gateway's AI reader for balance-sheet documents. It runs when the ordinary
// reader cannot help: a scanned PDF (its text is read with Amazon Textract), a document with no table the
// reader recognises, or a statement whose line items match none of the engine's AI-spend keywords.
//
// It never reads money. A model (Claude on Amazon Bedrock) points at the line items that look like AI or
// technology spending and copies each amount and unit as printed; every proposal is then checked against
// the page text, and what survives becomes a small CSV the rules engine parses like any other. The person
// still decides which rows count. Readings are kept in memory for an hour, keyed by the file's SHA-256, so
// "confirm" sees exactly the rows "parse" proposed; no document content is logged.
package aiassist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"c23desk/internal/docparse"
)

// Proposal is one line item the model says is AI or technology spending, copied as the page prints it.
type Proposal struct {
	Page     int    `json:"page"`
	Table    string `json:"table"`
	Label    string `json:"label"`
	Column   string `json:"column"`
	Amount   string `json:"amount"`
	Unit     string `json:"unit"`
	Currency string `json:"currency"`
	Category string `json:"category"`
	Why      string `json:"why"`
}

// Finder asks a model for proposals on the given pages.
type Finder interface {
	Find(ctx context.Context, pages []docparse.PageText) ([]Proposal, error)
	Model() string
}

// OCR reads the text of a scanned PDF, page by page.
type OCR interface {
	Pages(ctx context.Context, pdf []byte) ([]docparse.PageText, error)
	Name() string
}

// Config limits what one reading may cost.
type Config struct {
	MaxPagesToModel int           // most pages sent to the model (default 12)
	MaxCharsToModel int           // most characters of page text sent (default 120,000)
	OCRMaxPages     int           // largest scanned PDF sent to OCR, in pages (default 100)
	CacheTTL        time.Duration // how long a reading is kept for confirm (default 1h)
	DocMaxPages     int           // most PDF pages read for text (default 500)
}

// Trigger says why the AI reader ran.
const (
	TriggerScanned      = "scanned_document" // the PDF has no text layer
	TriggerNoTable      = "no_table_found"   // text, but no description-and-amount table
	TriggerNoCandidates = "no_candidates"    // a table, but no line with an amount matched the AI-spend keywords
	TriggerRequested    = "requested"        // the person asked for it
)

// Dropped is a proposal that failed the checks, and why.
type Dropped struct {
	Page   int    `json:"page"`
	Label  string `json:"label"`
	Amount string `json:"amount"`
	Reason string `json:"reason"`
}

// Info describes a reading for the person reviewing it (extraction.ai_assist in the answer).
type Info struct {
	Used        bool      `json:"used"`
	Trigger     string    `json:"trigger,omitempty"`
	Method      string    `json:"method,omitempty"` // "document text + <model>" or "<ocr> + <model>"
	Model       string    `json:"model,omitempty"`
	OCR         string    `json:"ocr,omitempty"`
	PagesRead   int       `json:"pages_read,omitempty"` // pages with text
	PagesSent   []int     `json:"pages_sent,omitempty"` // pages (or sheet numbers) shown to the model
	Proposed    int       `json:"proposed"`
	Kept        int       `json:"kept"`
	Dropped     []Dropped `json:"dropped,omitempty"`
	Notes       []string  `json:"notes,omitempty"`
	Cached      bool      `json:"cached,omitempty"`
	Error       string    `json:"error,omitempty"` // set when the AI reader ran and could not help
	KeptForMins int       `json:"kept_for_minutes,omitempty"`
}

// Error is a reading that could not produce rows, with a reason a person can act on.
type Error struct {
	Code    string
	Message string
	Info    *Info
}

func (e *Error) Error() string { return e.Message }

// Error codes.
const (
	CodeUnavailable = "ai_reader_unavailable" // not configured for this kind of document
	CodeTooManyOCR  = "ai_reader_too_many_pages"
	CodeNothing     = "ai_reader_found_nothing"
	CodeFailed      = "ai_reader_failed"
	CodeExpired     = "ai_reading_expired"
)

// Request is one document to read.
type Request struct {
	Format  string
	Doc     []byte
	Opt     docparse.Options
	Scanned bool   // the ordinary reader found no text layer: use OCR
	Trigger string // why it runs
}

// Key identifies a reading: the same bytes read with the same page and sheet choices.
func Key(format string, doc []byte, opt docparse.Options) string {
	sum := sha256.Sum256(doc)
	return format + "|" + hex.EncodeToString(sum[:]) + "|" + strings.TrimSpace(opt.Pages) + "|" + opt.Sheet
}

// Reader runs the AI reading. A nil OCR means scanned documents cannot be read.
type Reader struct {
	finder Finder
	ocr    OCR
	cfg    Config

	mu    sync.Mutex
	cache map[string]cached
	now   func() time.Time
}

type cached struct {
	res *docparse.Result
	inf Info
	exp time.Time
}

func New(finder Finder, ocr OCR, cfg Config) *Reader {
	if cfg.MaxPagesToModel <= 0 {
		cfg.MaxPagesToModel = 12
	}
	if cfg.MaxCharsToModel <= 0 {
		cfg.MaxCharsToModel = 120_000
	}
	if cfg.OCRMaxPages <= 0 {
		cfg.OCRMaxPages = 100
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = time.Hour
	}
	return &Reader{finder: finder, ocr: ocr, cfg: cfg, cache: map[string]cached{}, now: time.Now}
}

// CanOCR reports whether scanned PDFs can be read.
func (r *Reader) CanOCR() bool { return r != nil && r.ocr != nil }

// Cached returns the reading stored under key, if it has not expired.
func (r *Reader) Cached(key string) (*docparse.Result, *Info, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.cache[key]
	if !ok || r.now().After(c.exp) {
		delete(r.cache, key)
		return nil, nil, false
	}
	inf := c.inf
	inf.Cached = true
	return c.res, &inf, true
}

func (r *Reader) store(key string, res *docparse.Result, inf Info) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for k, c := range r.cache {
		if now.After(c.exp) {
			delete(r.cache, k)
		}
	}
	if len(r.cache) >= 64 { // a handful of people review at once; drop the oldest rather than grow
		oldest, at := "", time.Time{}
		for k, c := range r.cache {
			if oldest == "" || c.exp.Before(at) {
				oldest, at = k, c.exp
			}
		}
		delete(r.cache, oldest)
	}
	r.cache[key] = cached{res: res, inf: inf, exp: now.Add(r.cfg.CacheTTL)}
}

// Read proposes AI-spend rows for a document, from the cache when the same reading was done recently.
func (r *Reader) Read(ctx context.Context, q Request) (*docparse.Result, *Info, error) {
	key := Key(q.Format, q.Doc, q.Opt)
	if res, inf, ok := r.Cached(key); ok {
		return res, inf, nil
	}
	inf := &Info{Used: true, Trigger: q.Trigger, Model: r.finder.Model(), KeptForMins: int(r.cfg.CacheTTL / time.Minute)}
	failWith := func(code, format string, a ...any) (*docparse.Result, *Info, error) {
		inf.Used = false
		inf.Error = fmt.Sprintf(format, a...)
		return nil, inf, &Error{Code: code, Message: inf.Error, Info: inf}
	}

	var pages []docparse.PageText
	if q.Scanned {
		if r.ocr == nil {
			return failWith(CodeUnavailable, "this PDF is a scan and reading scans (OCR) is not set up on this server")
		}
		total, err := docparse.PageCount(q.Doc)
		if err != nil {
			return failWith(CodeFailed, "%s", err.Error())
		}
		if total > r.cfg.OCRMaxPages {
			return failWith(CodeTooManyOCR, "this scanned PDF has %d pages and at most %d are sent to OCR at once; upload only the pages that hold the statement", total, r.cfg.OCRMaxPages)
		}
		inf.OCR = r.ocr.Name()
		inf.Method = r.ocr.Name() + " + " + r.finder.Model()
		all, err := r.ocr.Pages(ctx, q.Doc)
		if err != nil {
			return failWith(CodeFailed, "OCR could not read the scan: %s", err.Error())
		}
		pages = all
		if strings.TrimSpace(q.Opt.Pages) != "" {
			want, err := docparse.SelectPages(q.Opt.Pages, total)
			if err != nil {
				return failWith(CodeFailed, "%s", err.Error())
			}
			keep := map[int]bool{}
			for _, p := range want {
				keep[p] = true
			}
			pages = pages[:0:0]
			for _, p := range all {
				if keep[p.Page] {
					pages = append(pages, p)
				}
			}
		}
	} else {
		opt := q.Opt
		if opt.MaxPages == 0 {
			opt.MaxPages = r.cfg.DocMaxPages
		}
		inf.Method = "document text + " + r.finder.Model()
		var err error
		if pages, err = docparse.Texts(ctx, q.Format, q.Doc, opt); err != nil {
			return failWith(CodeFailed, "%s", err.Error())
		}
	}
	inf.PagesRead = len(pages)
	chosen := SelectPages(pages, r.cfg.MaxPagesToModel, r.cfg.MaxCharsToModel)
	if len(chosen) == 0 {
		return failWith(CodeNothing, "no page mentions software, cloud, AI or technology spending next to an amount, so there was nothing to ask the AI reader about")
	}
	for _, p := range chosen {
		inf.PagesSent = append(inf.PagesSent, p.Page)
	}

	props, err := r.finder.Find(ctx, chosen)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return failWith(CodeFailed, "the AI reader took too long; choose fewer pages and try again")
		}
		return failWith(CodeFailed, "the AI reader could not be reached: %s", err.Error())
	}
	inf.Proposed = len(props)
	rows, dropped, notes := Verify(props, chosen)
	inf.Dropped, inf.Kept = dropped, len(rows)
	inf.Notes = append(inf.Notes, notes...)
	if len(rows) == 0 {
		if len(props) == 0 {
			return failWith(CodeNothing, "the AI reader read %s and found no line item that shows AI or technology spending", pagePhrase(inf.PagesSent))
		}
		return failWith(CodeNothing, "the AI reader proposed %d line(s) but none could be checked against the page text, so none are shown", len(props))
	}
	res := buildResult(q.Format, rows, inf)
	r.store(key, res, *inf)
	return res, inf, nil
}

func pagePhrase(ps []int) string {
	if len(ps) == 1 {
		return fmt.Sprintf("page %d", ps[0])
	}
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = fmt.Sprint(p)
	}
	return "pages " + strings.Join(s, ", ")
}

// ---- choosing pages ----

var (
	// Words that name technology spending. "Digital" and "platform" are left out on purpose: in a bank's
	// report they name business lines ("Digital Banking" revenue), not spending.
	techWordRe = regexp.MustCompile(`(?i)\b(?:software|cloud|licen[cs]es?|subscriptions?|saas|artificial intelligence|machine learning|gen(?:erative)?[ -]?ai|ai|llm|tokens?|information technology|it (?:expenses?|costs?|infrastructure|services|assets)|technology|data cent(?:re|er)s?|computers?|hardware|servers?|cyber ?security)\b`)
	// A table row ends in an amount, after a cell separator: " | " (PDF and workbook text) or a comma (CSV).
	tableRowRe  = regexp.MustCompile(`[|,]\s*"?\(?-?\d[\d,]*(?:\.\d+)?\)?"?\s*$`)
	moneyRe     = regexp.MustCompile(`\(?-?\d{1,3}(?:,\d{2,3})+(?:\.\d+)?\)?|\b\d+\.\d+\b|\b\d{4,}\b`)
	unitStateRe = regexp.MustCompile(`(?i)(?:in\s+(?:'|‘|’)?000s?|in\s+(?:lakhs?|crores?|millions?|billions?|thousands?|rupees|inr)|₹|\brs\.?\s|\binr\b)`)
)

// SelectPages picks the pages most likely to hold technology spending, within the page and character
// budget, returned in page order. What counts is technology named inside a table (a line with cells and
// an amount) or in the few lines just above one, where its title sits; an annual report's narrative
// mentions technology on page after page and is weighed lightly.
func SelectPages(pages []docparse.PageText, maxPages, maxChars int) []docparse.PageText {
	type scored struct {
		i     int
		score float64
	}
	var cands []scored
	for i, p := range pages {
		if s := pageScore(p.Text); s > 0 {
			cands = append(cands, scored{i, s})
		}
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].score > cands[b].score })
	var pick []int
	chars := 0
	for _, c := range cands {
		if len(pick) >= maxPages {
			break
		}
		n := len(pages[c.i].Text)
		if chars+n > maxChars {
			continue
		}
		pick, chars = append(pick, c.i), chars+n
	}
	out := make([]docparse.PageText, len(pick))
	for k, i := range pick {
		out[k] = pages[i]
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Page < out[b].Page })
	return out
}

// pageScore is 0 for a page with no table, or no technology named in or just above one.
func pageScore(text string) float64 {
	lines := strings.Split(text, "\n")
	table := make([]bool, len(lines))
	tableLines := 0
	for k, l := range lines {
		// a table row: its last cell is an amount (narrative set in two columns has cells too, but its
		// lines rarely end in a figure)
		if tableRowRe.MatchString(l) {
			table[k], tableLines = true, tableLines+1
		}
	}
	if tableLines == 0 {
		return 0
	}
	score := 0.0
	for k, l := range lines {
		hits := float64(len(techWordRe.FindAllStringIndex(l, -1)))
		if hits == 0 {
			continue
		}
		heading := false
		for j := k + 1; j <= k+6 && j < len(lines); j++ {
			if table[j] {
				heading = true
				break
			}
		}
		switch {
		case table[k]:
			score += 3 * hits
		case heading:
			score += 3 * hits
		default:
			score += 0.25 * hits
		}
	}
	if score < 1 {
		return 0
	}
	if unitStateRe.MatchString(text) {
		score *= 2 // a page that states a unit holds a financial table
	}
	return score
}

// ---- checking proposals ----

// Row is a proposal that passed the checks.
type Row struct {
	Proposal
	Number   string // the amount's digits as printed (grouping kept), sign excluded
	Negative bool
	Sheet    string
}

var (
	numberRe = regexp.MustCompile(`\d[\d,]*(?:\.\d+)?`)
	wordRe   = regexp.MustCompile(`[\p{L}]{4,}`)
	units    = map[string]string{"rupees": "", "thousands": "k", "lakh": "L", "crore": "cr", "millions": "mn", "billions": "bn", "unstated": ""}
)

func digits(s string) string { return strings.ReplaceAll(s, ",", "") }

// Verify keeps the proposals whose page was shown to the model, whose amount appears on that page as a
// number, whose description appears there too, and whose unit the page does not contradict.
func Verify(props []Proposal, pages []docparse.PageText) (rows []Row, dropped []Dropped, notes []string) {
	byPage := map[int]docparse.PageText{}
	for _, p := range pages {
		byPage[p.Page] = p
	}
	seen := map[string]bool{}
	unitNotes := 0
	for _, pr := range props {
		pr.Label = strings.Join(strings.Fields(pr.Label), " ")
		pr.Table = strings.Join(strings.Fields(pr.Table), " ")
		pr.Unit = strings.ToLower(strings.TrimSpace(pr.Unit))
		drop := func(why string) {
			dropped = append(dropped, Dropped{Page: pr.Page, Label: pr.Label, Amount: pr.Amount, Reason: why})
		}
		pg, ok := byPage[pr.Page]
		if !ok {
			drop("the page it names was not one of the pages read")
			continue
		}
		if c := strings.ToUpper(strings.TrimSpace(pr.Currency)); c != "" && c != "INR" && c != "₹" && c != "RS" && c != "RUPEES" {
			drop("the amount is not in rupees")
			continue
		}
		if pr.Label == "" {
			drop("no description")
			continue
		}
		num := numberRe.FindString(pr.Amount)
		if num == "" {
			drop("no amount")
			continue
		}
		if !numberOnPage(num, pg.Text) {
			drop("the amount does not appear on the page")
			continue
		}
		if !labelOnPage(pr.Label, pg.Text) {
			drop("the description does not appear on the page")
			continue
		}
		if _, known := units[pr.Unit]; !known {
			pr.Unit = "unstated"
		}
		stated := map[string]bool{}
		for _, line := range strings.Split(pg.Text, "\n") {
			for _, u := range docparse.UnitsIn(line) {
				stated[u] = true
			}
		}
		switch {
		case pr.Unit != "unstated" && stated[pr.Unit]:
		case len(stated) == 1:
			for u := range stated {
				if pr.Unit != u {
					unitNotes++
				}
				pr.Unit = u // the page states one unit: it wins over the model's reading
			}
		case len(stated) > 1:
			drop(fmt.Sprintf("the page states more than one unit and the reader's %q is not one of them", pr.Unit))
			continue
		}
		neg := strings.Contains(pr.Amount, "(") && strings.Contains(pr.Amount, ")") || strings.HasPrefix(strings.TrimSpace(pr.Amount), "-")
		k := fmt.Sprint(pr.Page, "|", strings.ToLower(pr.Label), "|", digits(num))
		if seen[k] {
			continue
		}
		seen[k] = true
		rows = append(rows, Row{Proposal: pr, Number: num, Negative: neg, Sheet: pg.Sheet})
	}
	if unitNotes > 0 {
		notes = append(notes, fmt.Sprintf("The unit of %d row(s) was taken from the page, which states it, instead of the AI reader's reading.", unitNotes))
	}
	return rows, dropped, notes
}

func numberOnPage(num, text string) bool {
	want := digits(num)
	for _, m := range numberRe.FindAllString(text, -1) {
		if digits(m) == want {
			return true
		}
	}
	return false
}

// labelOnPage: at least half of the description's words of four or more letters are on the page (all
// of it, for a short description such as "IT"), so a reworded or invented line is not shown.
func labelOnPage(label, text string) bool {
	low := strings.ToLower(text)
	words := wordRe.FindAllString(strings.ToLower(label), -1)
	if len(words) == 0 {
		return strings.Contains(low, strings.ToLower(label))
	}
	found := 0
	for _, w := range words {
		if strings.Contains(low, w) {
			found++
		}
	}
	return found*2 >= len(words)
}

// ---- the CSV the engine reads ----

func buildResult(format string, rows []Row, inf *Info) *docparse.Result {
	var b strings.Builder
	b.WriteString("Particulars,Amount as printed\n")
	res := &docparse.Result{Format: format, Rows: len(rows), Lines: []docparse.Source{{}}, Reasons: []string{""}}
	unstated := 0
	for _, r := range rows {
		label := r.Label
		if r.Table != "" && !strings.Contains(strings.ToLower(label), strings.ToLower(r.Table)) {
			label += " (" + r.Table + ")"
		}
		label = trimRunes(label, 160)
		amount := r.Number
		if s := units[r.Unit]; s != "" {
			amount += " " + s // the engine applies the unit: an inline suffix wins over the column's
		}
		if r.Negative {
			amount = "-" + amount
		}
		if r.Unit == "unstated" {
			unstated++
		}
		b.WriteString(csvCell(label) + "," + csvCell(amount) + "\n")
		src := docparse.Source{Page: r.Page, Text: trimRunes(strings.TrimSpace(r.Table+" · "+r.Column+": "+r.Label+" "+r.Amount), 200)}
		if r.Sheet != "" {
			src = docparse.Source{Sheet: r.Sheet, Text: src.Text}
		}
		res.Lines = append(res.Lines, src)
		why := strings.TrimSpace(r.Why)
		cat := strings.ReplaceAll(strings.TrimSpace(r.Category), "_", " ")
		reason := "Proposed by the AI reader"
		if cat != "" {
			reason += " (" + cat + ")"
		}
		if why != "" {
			reason += ": " + trimRunes(why, 200)
		}
		res.Reasons = append(res.Reasons, reason)
	}
	res.CSV = []byte(b.String())
	res.Flags = append(res.Flags, docparse.Flag{Code: "AI_READER_PROPOSED", Message: fmt.Sprintf(
		"These rows were proposed by an AI reader (%s) from %s. Each amount and description was checked to appear on its page, but the choice of lines and units can still be wrong: compare every row with the original before confirming.",
		inf.Method, pagePhrase(inf.PagesSent))})
	if unstated > 0 {
		res.Flags = append(res.Flags, docparse.Flag{Code: "AI_UNIT_UNSTATED", Message: fmt.Sprintf("%d proposed row(s) come from a page that states no unit, so they were read as rupees. If the page's figures are in thousands, lakh, crore or millions, choose that unit and read again.", unstated)})
	}
	if len(inf.Dropped) > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("%d of the AI reader's proposals were left out because they could not be checked against the page text.", len(inf.Dropped)))
	}
	res.Notes = append(res.Notes, inf.Notes...)
	return res
}

func csvCell(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
