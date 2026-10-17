// Package fixture loads the C23 fixture shape (+ optional team extension) into validated pilots.
// Nothing is imputed. Invalid rows are rejected with structured errors; valid rows are returned.
package fixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"c23desk/internal/domain"
	"c23desk/internal/money"
)

// ErrMalformed is returned for unparseable JSON or a wrong top-level shape.
var ErrMalformed = errors.New("malformed input")

// Rejected is one invalid row.
type Rejected struct {
	Row    int                 `json:"row"`
	ID     string              `json:"id"`
	Errors []domain.FieldError `json:"errors"`
}

// Result of loading.
type Result struct {
	Pilots   []domain.Pilot      `json:"pilots"`
	Rejected []Rejected          `json:"rejected"`
	Warnings []string            `json:"warnings"`
	Budget   money.Amount        `json:"pilot_budget_inr"`
	BudgetOK bool                `json:"-"`
	Meta     map[string]any      `json:"meta"`
	Errors   []domain.FieldError `json:"file_errors"`
}

// Rows extracts the pilot array from {records:{pilots}}, {pilots}, or a bare array.
func Rows(b []byte) ([]json.RawMessage, map[string]any, error) {
	t := bytes.TrimSpace(b)
	if len(t) == 0 {
		return nil, nil, fmt.Errorf("%w: empty body", ErrMalformed)
	}
	if t[0] == '[' {
		var rows []json.RawMessage
		if err := json.Unmarshal(t, &rows); err != nil {
			return nil, nil, fmt.Errorf("%w: %v", ErrMalformed, err)
		}
		return rows, map[string]any{}, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(t, &top); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	meta := map[string]any{}
	for k, v := range top {
		if k == "records" || k == "pilots" {
			continue
		}
		var x any
		_ = json.Unmarshal(v, &x)
		meta[k] = x
	}
	var src json.RawMessage
	if r, ok := top["records"]; ok {
		var rec map[string]json.RawMessage
		if json.Unmarshal(r, &rec) == nil {
			src = rec["pilots"]
		}
	} else {
		src = top["pilots"]
	}
	var rows []json.RawMessage
	if src == nil || json.Unmarshal(src, &rows) != nil {
		return nil, nil, fmt.Errorf("%w: expected records.pilots array, pilots array, or a top-level array", ErrMalformed)
	}
	return rows, meta, nil
}

// Load validates fixture rows (+ extension rows). `source` labels supplied core values.
func Load(fixtureJSON, extensionJSON []byte, source string) (Result, error) {
	rows, meta, err := Rows(fixtureJSON)
	if err != nil {
		return Result{}, err
	}
	res := Result{Meta: meta, Pilots: []domain.Pilot{}, Rejected: []Rejected{}, Warnings: []string{}, Errors: []domain.FieldError{}}
	ext := map[string][]map[string]json.RawMessage{}
	if len(bytes.TrimSpace(extensionJSON)) > 0 {
		erows, emeta, err := Rows(extensionJSON)
		if err != nil {
			return Result{}, fmt.Errorf("extension: %w", err)
		}
		if b, ok := emeta["pilot_budget_inr"]; ok {
			raw, _ := json.Marshal(b)
			a, code := money.ParseAmount(raw)
			if code != "" {
				res.Errors = append(res.Errors, domain.FieldError{Field: "pilot_budget_inr", Code: code, Message: "pilot_budget_inr must be a non-negative INR number"})
			} else {
				res.Budget, res.BudgetOK = a, a.Known
			}
		}
		for i, r := range erows {
			var o map[string]json.RawMessage
			if json.Unmarshal(r, &o) != nil || o == nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("extension row %d is not an object; ignored", i))
				continue
			}
			var id string
			_ = json.Unmarshal(o["id"], &id)
			ext[strings.ToLower(strings.TrimSpace(id))] = append(ext[strings.ToLower(strings.TrimSpace(id))], o)
		}
	}

	type parsed struct {
		row  int
		id   string
		p    domain.Pilot
		errs []domain.FieldError
	}
	var all []parsed
	seen := map[string]bool{}
	for i, r := range rows {
		obj, derrs := domain.DecodeObject(r)
		if derrs != nil {
			all = append(all, parsed{row: i, errs: derrs})
			continue
		}
		var id string
		_ = json.Unmarshal(obj["id"], &id)
		key := strings.ToLower(strings.TrimSpace(id))
		seen[key] = true
		merged, extKeys, errs := obj, map[string]bool{}, []domain.FieldError(nil)
		if es := ext[key]; len(es) > 0 && key != "" {
			if len(es) > 1 {
				errs = append(errs, domain.FieldError{Field: "extension.id", Code: domain.CodeDuplicateID, Message: "more than one extension row for this pilot"})
			}
			var warns []string
			var merr []domain.FieldError
			merged, warns, merr = domain.MergeObjects(obj, es[0])
			errs = append(errs, merr...)
			for _, w := range warns {
				res.Warnings = append(res.Warnings, id+": "+w)
			}
			for k := range es[0] {
				name := k
				if a, ok := domain.ExtensionAliases[k]; ok {
					name = a
				}
				if _, inFixture := obj[name]; !inFixture {
					extKeys[name] = true
				}
			}
		}
		p, perrs := domain.BuildPilot(merged, source)
		errs = append(errs, perrs...)
		for k := range extKeys {
			if p.Sources != nil && p.Sources[k] != domain.SourceUnknown && !strings.HasPrefix(k, "actual_") {
				p.Sources[k] = domain.SourceTeam
			}
		}
		all = append(all, parsed{row: i, id: strings.TrimSpace(id), p: p, errs: errs})
	}
	// duplicate IDs (case-insensitive): reject EVERY occurrence so the outcome is order-independent
	count := map[string]int{}
	for _, a := range all {
		if a.id != "" {
			count[strings.ToLower(a.id)]++
		}
	}
	for _, a := range all {
		if a.id != "" && count[strings.ToLower(a.id)] > 1 {
			a.errs = append(a.errs, domain.FieldError{Field: "id", Code: domain.CodeDuplicateID, Message: fmt.Sprintf("id %q appears more than once; all occurrences rejected", a.id)})
		}
		if len(a.errs) > 0 {
			row := a.row
			for j := range a.errs {
				a.errs[j].Row = &row
			}
			res.Rejected = append(res.Rejected, Rejected{Row: a.row, ID: a.id, Errors: a.errs})
			continue
		}
		res.Pilots = append(res.Pilots, a.p)
	}
	orphans := []string{}
	for k := range ext {
		if !seen[k] {
			orphans = append(orphans, k)
		}
	}
	sort.Strings(orphans)
	for _, o := range orphans {
		res.Warnings = append(res.Warnings, fmt.Sprintf("ORPHAN_EXTENSION_ROW: %q matches no pilot; ignored", o))
	}
	if len(rows) == 0 {
		res.Warnings = append(res.Warnings, "EMPTY_LIST: no pilot rows supplied")
	}
	return res, nil
}

// RawManifest mirrors data/raw/MANIFEST.json.
type RawManifest struct {
	Files []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
		Source string `json:"source"`
	} `json:"files"`
}

// LoadOfficial loads data/raw/official_fixture.json + extension.json after verifying SHA-256 against MANIFEST.json.
// Only this path labels values OFFICIAL_FIXTURE.
func LoadOfficial(rawDir string) (Result, error) {
	mb, err := os.ReadFile(filepath.Join(rawDir, "MANIFEST.json"))
	if err != nil {
		return Result{}, err
	}
	var m RawManifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return Result{}, err
	}
	files := map[string][]byte{}
	for _, f := range m.Files {
		b, err := os.ReadFile(filepath.Join(rawDir, f.Path))
		if err != nil {
			return Result{}, err
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			return Result{}, fmt.Errorf("integrity check failed: %s does not match MANIFEST.json", f.Path)
		}
		files[f.Path] = b
	}
	if files["official_fixture.json"] == nil {
		return Result{}, errors.New("official_fixture.json missing from manifest")
	}
	return Load(files["official_fixture.json"], files["extension.json"], domain.SourceOfficial)
}
