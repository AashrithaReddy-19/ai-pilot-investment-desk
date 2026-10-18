package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"c23desk/internal/aiassist"
	"c23desk/internal/config"
	"c23desk/internal/docparse"
)

// The AI reader through the gateway: it runs only when the ordinary reader cannot help, its rows reach the
// engine as propose=all, confirm reuses the same reading, and every way it can fail is explained.

type aiFinder struct {
	mu    sync.Mutex
	props []aiassist.Proposal
	err   error
	calls int
}

func (f *aiFinder) Find(context.Context, []docparse.PageText) ([]aiassist.Proposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.props, f.err
}
func (f *aiFinder) Model() string { return "test-model" }

func newAIStatementRouter(f *fakeRules, finder aiassist.Finder) http.Handler {
	cfg := config.Config{
		AllowedOrigins: []string{"http://localhost:5173"}, RequestTimeout: 2 * time.Second,
		MaxBodyBytes: 1 << 20, DocumentMaxBytes: 4 << 20, DocumentMaxPages: 50, DocumentTimeout: 20 * time.Second, DocumentConcurrency: 2,
		AITimeout: 10 * time.Second,
	}
	return NewRouter(Deps{Cfg: cfg, Store: fakeStore{}, Rules: f, Cache: fakeCache{}, Log: slog.New(slog.NewTextHandler(&lockedBuf{}, nil)),
		AI: aiassist.New(finder, nil, aiassist.Config{})})
}

// No line matches a keyword: "OpenAI" is not the word "AI", and "subscription" is not one of them.
const noKeywordCSV = "Particulars,Amount\nRent,\"1,000\"\nOpenAI subscription,\"1,20,000\"\n"

func openAIFinder() *aiFinder {
	return &aiFinder{props: []aiassist.Proposal{{Page: 1, Label: "OpenAI subscription", Amount: "1,20,000", Unit: "rupees", Currency: "INR", Category: "ai", Why: "an AI model subscription"}}}
}

func extractionOf(m map[string]any) map[string]any {
	x, _ := m["extraction"].(map[string]any)
	return x
}

func TestAIReaderProposesWhenNoKeywordMatchesAndConfirmReusesIt(t *testing.T) {
	eng := &statementEngine{}
	finder := openAIFinder()
	h := newAIStatementRouter(eng.fake(), finder)

	w := upload(h, "balance-sheet/parse", map[string]string{"unit": "auto"}, "bs.csv", []byte(noKeywordCSV))
	m := parseOut(t, w)
	cands := candidatesOf(t, m)
	if w.Code != 200 || len(cands) != 1 || cands[0]["label"] != "OpenAI subscription" || cands[0]["amount_inr"] != 120000.0 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if r, _ := cands[0]["reason"].(string); !strings.HasPrefix(r, "Proposed by the AI reader (ai)") {
		t.Fatalf("the row says the AI reader proposed it: %q", r)
	}
	x := extractionOf(m)
	ai, _ := x["ai_assist"].(map[string]any)
	if x["ai_available"] != true || ai == nil || ai["used"] != true || ai["trigger"] != aiassist.TriggerNoCandidates || ai["kept"] != 1.0 {
		t.Fatalf("extraction: %+v", x)
	}
	if !has(flagCodes(m), "AI_READER_PROPOSED") {
		t.Fatalf("flags: %v", flagCodes(m))
	}
	if eng.fields["propose"] != "all" || eng.fields["ai_assist"] != "" || !strings.HasPrefix(eng.parses[1], "Particulars,Amount as printed\n") {
		t.Fatalf("the engine gets the AI reader's table with propose=all and never ai_assist: %v %q", eng.fields, eng.parses)
	}

	// confirm with ai_assist=on reads the same rows, without asking the model again
	w = upload(h, "balance-sheet/confirm", map[string]string{"rows": "2", "ai_assist": "on"}, "bs.csv", []byte(noKeywordCSV))
	if w.Code != 200 || finder.calls != 1 || eng.confirms[0] != eng.parses[1] {
		t.Fatalf("confirm: %d calls=%d\n%s", w.Code, finder.calls, w.Body.String())
	}
	if ai, _ := extractionOf(parseOut(t, w))["ai_assist"].(map[string]any); ai["cached"] != true {
		t.Fatalf("confirm says the reading was reused: %+v", ai)
	}
	// a file the AI reader never read cannot be confirmed through it
	other := strings.Replace(noKeywordCSV, "1,000", "2,000", 1)
	errCode(t, upload(h, "balance-sheet/confirm", map[string]string{"rows": "2", "ai_assist": "on"}, "bs.csv", []byte(other)), http.StatusConflict, aiassist.CodeExpired)
}

func TestKeywordRowsWithoutAmountsStillCallTheAIReader(t *testing.T) {
	eng := &statementEngine{}
	finder := openAIFinder()
	h := newAIStatementRouter(eng.fake(), finder)
	// "AI" matches, but the row has no amount: nothing could be counted, so the AI reader looks
	csvIn := noKeywordCSV + "AI et ec,\n"
	w := upload(h, "balance-sheet/parse", nil, "bs.csv", []byte(csvIn))
	if w.Code != 200 || finder.calls != 1 {
		t.Fatalf("%d calls=%d %s", w.Code, finder.calls, w.Body.String())
	}
}

func TestKeywordMatchesDoNotCallTheAIReader(t *testing.T) {
	eng := &statementEngine{}
	finder := openAIFinder()
	h := newAIStatementRouter(eng.fake(), finder)
	w := upload(h, "balance-sheet/parse", nil, "bs.csv", []byte("Particulars,Amount\nSoftware licences,\"1,000\"\n"))
	x := extractionOf(parseOut(t, w))
	if w.Code != 200 || finder.calls != 0 || x["ai_available"] != true || x["ai_assist"] != nil {
		t.Fatalf("%d calls=%d %+v", w.Code, finder.calls, x)
	}
	// ai_assist=off never calls it, even with nothing found
	if w := upload(h, "balance-sheet/parse", map[string]string{"ai_assist": "off"}, "bs.csv", []byte(noKeywordCSV)); w.Code != 200 || finder.calls != 0 {
		t.Fatalf("off: %d calls=%d", w.Code, finder.calls)
	}
	// ai_assist=on asks for it even when keywords matched
	w = upload(h, "balance-sheet/parse", map[string]string{"ai_assist": "on"}, "bs.csv", []byte(noKeywordCSV))
	if ai, _ := extractionOf(parseOut(t, w))["ai_assist"].(map[string]any); w.Code != 200 || finder.calls != 1 || ai["trigger"] != aiassist.TriggerRequested {
		t.Fatalf("on: %d calls=%d %+v", w.Code, finder.calls, ai)
	}
	errCode(t, upload(h, "balance-sheet/parse", map[string]string{"ai_assist": "maybe"}, "bs.csv", []byte(noKeywordCSV)), 422, "invalid_value")
}

func TestAIReaderFailureKeepsTheOrdinaryAnswerAndSaysWhy(t *testing.T) {
	eng := &statementEngine{}
	h := newAIStatementRouter(eng.fake(), &aiFinder{err: errors.New("Bedrock answered 403")})
	w := upload(h, "balance-sheet/parse", nil, "bs.csv", []byte(noKeywordCSV))
	m := parseOut(t, w)
	ai, _ := extractionOf(m)["ai_assist"].(map[string]any)
	if w.Code != 200 || len(candidatesOf(t, m)) != 0 || ai["used"] != false || !strings.Contains(ai["error"].(string), "403") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	// asked for explicitly, the failure is the answer
	errCode(t, upload(h, "balance-sheet/parse", map[string]string{"ai_assist": "on"}, "bs.csv", []byte(noKeywordCSV)), http.StatusBadGateway, aiassist.CodeFailed)
}

func TestScanWithoutOCRSaysTheAIReaderCouldNotHelpEither(t *testing.T) {
	eng := &statementEngine{}
	h := newAIStatementRouter(eng.fake(), openAIFinder())
	scan := pdfOf([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	})
	e := errCode(t, upload(h, "balance-sheet/parse", nil, "scan.pdf", scan), 422, docparse.CodeScanned)
	if msg := e["message"].(string); !strings.Contains(msg, "The AI reader could not help either") || !strings.Contains(msg, "OCR") {
		t.Fatalf("message: %q", msg)
	}
}

func TestAIReaderOffServerRefusesAIAssistOn(t *testing.T) {
	eng := &statementEngine{}
	h := newStatementRouter(eng.fake(), &lockedBuf{}, nil)
	errCode(t, upload(h, "balance-sheet/parse", map[string]string{"ai_assist": "on"}, "bs.csv", []byte(noKeywordCSV)), 422, aiassist.CodeUnavailable)
	w := upload(h, "balance-sheet/parse", nil, "bs.csv", []byte(noKeywordCSV))
	if x := extractionOf(parseOut(t, w)); w.Code != 200 || x["ai_available"] != false {
		t.Fatalf("%d %+v", w.Code, x)
	}
}
