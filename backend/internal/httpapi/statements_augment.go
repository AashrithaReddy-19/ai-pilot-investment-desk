package httpapi

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"

	"c23desk/internal/aiassist"
	"c23desk/internal/docparse"
)

// What the gateway adds to the engine's balance-sheet answer. Everything is additive: every field the
// engine sent is kept exactly as it sent it, and an answer without candidates is returned untouched.

type extractionInfo struct {
	SourceFormat   string                   `json:"source_format"`
	Sheet          string                   `json:"sheet,omitempty"`
	Sheets         []docparse.SheetInfo     `json:"sheets,omitempty"`
	Pages          *docparse.PageInfo       `json:"pages,omitempty"`
	DroppedColumns []docparse.DroppedColumn `json:"dropped_columns,omitempty"`
	Currency       string                   `json:"currency,omitempty"`
	UnitDetected   string                   `json:"unit_detected,omitempty"`
	UnitSource     string                   `json:"unit_source,omitempty"`
	ConvertedRows  int                      `json:"converted_rows,omitempty"`
	Notes          []string                 `json:"notes,omitempty"`
	SourcesMatched bool                     `json:"sources_matched"`
	// AI reader: whether this server has one (so the reviewer can ask it to look), and what it did.
	AIAvailable bool           `json:"ai_available"`
	AI          *aiassist.Info `json:"ai_assist,omitempty"`
}

func rawJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// augmentStatement adds, to a parse or confirm answer: an "extraction" summary, a source (sheet and row,
// or page) and a reason on each row, and the flags a reviewer needs. form holds the fields the person sent;
// ai is what the AI reader did (nil when it did not run).
func augmentStatement(body []byte, res *docparse.Result, form map[string]string, ai *aiassist.Info, aiAvailable bool) []byte {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil || top == nil {
		return body
	}
	_, hasCandidates := top["candidates"]
	_, hasConfirmations := top["confirmations"]
	if !hasCandidates && !hasConfirmations {
		return body
	}

	var labels []string // label of each line of the converted CSV, to check the source references against
	if res != nil {
		if rs, err := csv.NewReader(bytes.NewReader(res.CSV)).ReadAll(); err == nil && len(rs) == len(res.Lines) {
			for _, r := range rs {
				labels = append(labels, strings.Join(strings.Fields(r[0]), " "))
			}
		}
	}
	allMatched := res != nil && labels != nil
	sourceOf := func(row int, label string) json.RawMessage {
		if res == nil || labels == nil || row < 1 || row > len(res.Lines) {
			allMatched = false
			return nil
		}
		if label != "" && strings.Join(strings.Fields(label), " ") != labels[row-1] {
			allMatched = false // the engine's row does not hold this description: show no source rather than a wrong one
			return nil
		}
		return rawJSON(res.Lines[row-1])
	}

	var extraFlags []docparse.Flag
	missing := 0
	rewrite := func(key string, withReason bool) {
		raw, ok := top[key]
		if !ok {
			return
		}
		var items []map[string]json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return
		}
		for _, it := range items {
			var row int
			var label string
			_ = json.Unmarshal(it["row"], &row)
			_ = json.Unmarshal(it["label"], &label)
			src := sourceOf(row, label)
			if src != nil {
				it["source"] = src
			}
			if withReason {
				var matched []string
				_ = json.Unmarshal(it["matched"], &matched)
				if len(matched) > 0 {
					it["reason"] = rawJSON("Matched keyword: " + strings.Join(matched, ", "))
				}
				if src != nil && row >= 1 && row <= len(res.Reasons) && res.Reasons[row-1] != "" {
					it["reason"] = rawJSON(res.Reasons[row-1]) // proposed by the AI reader, not by a keyword
				}
				if v, ok := it["amount_inr"]; ok && bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
					missing++
				}
			}
		}
		top[key] = rawJSON(items)
	}
	rewrite("candidates", true)
	rewrite("confirmations", false)

	format := docparse.FormatCSV
	info := extractionInfo{SourceFormat: format}
	if res != nil {
		info = extractionInfo{SourceFormat: res.Format, Sheet: res.Sheet, Sheets: res.Sheets, Pages: res.Pages,
			DroppedColumns: res.Dropped, Currency: res.Currency, UnitDetected: res.Unit, UnitSource: res.UnitSource,
			ConvertedRows: res.Rows, Notes: res.Notes}
		extraFlags = append(extraFlags, res.Flags...)
		if !allMatched {
			info.Notes = append(info.Notes, "Source references could not be matched to the rows the engine returned, so none are shown.")
		}
	}
	info.SourcesMatched = res != nil && allMatched
	info.AIAvailable, info.AI = aiAvailable, ai
	top["extraction"] = rawJSON(info)

	// the unit: if nothing stated one and the person did not choose, the figures were read as rupees
	var unit struct {
		Choice string `json:"choice"`
		Source string `json:"source"`
	}
	if json.Unmarshal(top["unit"], &unit) == nil && unit.Choice == "auto" && strings.HasPrefix(unit.Source, "default") {
		extraFlags = append(extraFlags, docparse.Flag{Code: "UNIT_ASSUMED", Message: "No unit is stated in the file, so the amounts were read as rupees. If the figures are in thousands, lakh or crore, choose that unit and parse again."})
	}
	if hasCandidates {
		var cols []string
		_ = json.Unmarshal(top["amount_columns"], &cols)
		var used string
		_ = json.Unmarshal(top["amount_column_used"], &used)
		if len(cols) > 1 && form["amount_column"] == "" {
			extraFlags = append(extraFlags, docparse.Flag{Code: "AMOUNT_COLUMN_CHOSEN", Message: fmt.Sprintf("The file has %d amount columns (%s) and %q was used. Choose the column that holds the figures you want to review and parse again.", len(cols), strings.Join(cols, ", "), used)})
		}
		if missing > 0 {
			extraFlags = append(extraFlags, docparse.Flag{Code: "AMOUNT_MISSING", Message: fmt.Sprintf("%d flagged row(s) have no readable amount. They cannot be counted until the amount is corrected in the source file.", missing)})
		}
	}
	if len(extraFlags) > 0 {
		var existing []json.RawMessage
		_ = json.Unmarshal(top["flags"], &existing)
		have := map[string]bool{}
		for _, e := range existing {
			var f struct {
				Code string `json:"code"`
			}
			if json.Unmarshal(e, &f) == nil {
				have[f.Code] = true
			}
		}
		for _, f := range extraFlags {
			if !have[f.Code] {
				existing = append(existing, rawJSON(f))
				have[f.Code] = true
			}
		}
		top["flags"] = rawJSON(existing)
	}
	out, err := json.Marshal(top)
	if err != nil {
		return body
	}
	return out
}
