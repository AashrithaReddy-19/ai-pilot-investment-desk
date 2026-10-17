package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"c23desk/internal/money"
)

// Validation codes (public API contract).
const (
	CodeRequired     = "REQUIRED"
	CodeNotText      = "NOT_TEXT"
	CodeEmpty        = "EMPTY"
	CodeTooLong      = "TOO_LONG"
	CodeBadID        = "INVALID_ID"
	CodeBadLabel     = "INVALID_LABEL"
	CodeInvalidEnum  = "INVALID_ENUM"
	CodeUnknownField = "UNKNOWN_FIELD"
	CodeDuplicateID  = "DUPLICATE_ID"
	CodeNotObject    = "NOT_AN_OBJECT"
)

var (
	idRE    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	labelRE = regexp.MustCompile(`^[a-z0-9][a-z0-9 _.&-]{0,63}$`)
	spaceRE = regexp.MustCompile(`\s+`)
)

// ReadinessLabels are the only accepted readiness labels.
var ReadinessLabels = map[string]bool{"low": true, "medium": true, "high": true}

// FieldError is one structured validation problem.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Row     *int   `json:"row,omitempty"`
	ID      string `json:"id,omitempty"`
}

// ValidID reports whether s is a safe pilot ID (also used for path parameters).
func ValidID(s string) bool { return idRE.MatchString(s) }

// NormalizeLabel trims, lower-cases and collapses whitespace.
func NormalizeLabel(s string) string {
	return spaceRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), " ")
}

// CanonicalBenefitType maps raw text to a canonical type. "time" is checked first: "cashable time" is still time.
func CanonicalBenefitType(raw *string) string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return BenefitUnknown
	}
	t := strings.ToLower(*raw)
	switch {
	case strings.Contains(t, "time"):
		return BenefitTimeFreed
	case strings.Contains(t, "cash"):
		return BenefitCashable
	default:
		return BenefitOther
	}
}

type kind int

const (
	kID kind = iota
	kText
	kLabel
	kMoney
	kPct
	kReadiness
)

type fieldSpec struct {
	name     string
	kind     kind
	required bool // must be present and non-null
}

// Pilot input schema: official core fields first, then documented product-extension fields.
var pilotFields = []fieldSpec{
	{"id", kID, true}, {"task", kText, true},
	{"setup_cost_inr", kMoney, false}, {"annual_running_cost_inr", kMoney, false}, {"annual_benefit_inr", kMoney, false},
	{"benefit_type", kLabel, false}, {"overlap_group", kLabel, false},
	{"description", kText, false},
	{"expected_adoption_pct", kPct, false}, {"actual_adoption_pct", kPct, false}, {"minimum_adoption_pct", kPct, false},
	{"expected_quality_pct", kPct, false}, {"actual_quality_pct", kPct, false}, {"minimum_quality_pct", kPct, false},
	{"data_readiness_pct", kPct, false}, {"technical_readiness_pct", kPct, false}, {"process_readiness_pct", kPct, false},
	{"capacity_value_inr", kMoney, false}, {"realized_cash_saving_inr", kMoney, false},
	{"readiness_label", kReadiness, false},
}

// ExtensionAliases maps legacy extension.json names to schema names.
var ExtensionAliases = map[string]string{
	"adoption_pct": "expected_adoption_pct", "observed_adoption_pct": "actual_adoption_pct",
	"observed_quality_pct": "actual_quality_pct", "readiness": "readiness_label",
}

var specByName = func() map[string]fieldSpec {
	m := map[string]fieldSpec{}
	for _, f := range pilotFields {
		m[f.name] = f
	}
	return m
}()

// ParsePilot validates a raw JSON object and builds a Pilot. Every field error is collected (not just the first).
// `source` labels the provenance of supplied core values; actual_* values are always OBSERVED.
// Values are never imputed: absent or null stays unknown.
func ParsePilot(raw json.RawMessage, source string) (Pilot, []FieldError) {
	obj, errs := decodeObject(raw)
	if errs != nil {
		return Pilot{}, errs
	}
	return BuildPilot(obj, source)
}

// CodeDuplicateKey: the same key twice in one object. Parsers disagree on which value wins, so it is rejected.
const CodeDuplicateKey = "DUPLICATE_KEY"

func decodeObject(raw json.RawMessage) (map[string]json.RawMessage, []FieldError) {
	return DecodeObject(raw)
}

// DecodeObject decodes one JSON object strictly: it must be an object and must not repeat a key.
func DecodeObject(raw json.RawMessage) (map[string]json.RawMessage, []FieldError) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || t[0] != '{' {
		return nil, []FieldError{{Field: "", Code: CodeNotObject, Message: "pilot must be a JSON object"}}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(t, &obj); err != nil {
		return nil, []FieldError{{Field: "", Code: CodeNotObject, Message: "pilot must be a JSON object"}}
	}
	if k, dup := duplicateKey(t); dup {
		return nil, []FieldError{{Field: k, Code: CodeDuplicateKey, Message: fmt.Sprintf("key %q appears more than once", k)}}
	}
	return obj, nil
}

// duplicateKey scans the top-level keys of a valid JSON object.
func duplicateKey(obj []byte) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(obj))
	if _, err := dec.Token(); err != nil { // '{'
		return "", false
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return "", false
		}
		k, _ := tok.(string)
		if seen[k] {
			return k, true
		}
		seen[k] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return "", false
		}
	}
	return "", false
}

// BuildPilot validates an already-decoded object (keys may include extension aliases resolved by the caller).
func BuildPilot(obj map[string]json.RawMessage, source string) (Pilot, []FieldError) {
	p := Pilot{Sources: map[string]string{}, BenefitType: BenefitUnknown}
	var errs []FieldError
	add := func(field, code, msg string) { errs = append(errs, FieldError{Field: field, Code: code, Message: msg}) }

	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, ok := specByName[k]; !ok && k != "version" {
			add(k, CodeUnknownField, fmt.Sprintf("field %q is not part of the pilot schema", k))
		}
	}

	for _, f := range pilotFields {
		rv, present := obj[f.name]
		isNull := !present || string(bytes.TrimSpace(rv)) == "null"
		if isNull {
			if f.required {
				add(f.name, CodeRequired, f.name+" is required")
			}
			if f.kind == kMoney || f.kind == kPct || f.kind == kReadiness || f.kind == kLabel {
				p.Sources[f.name] = SourceUnknown
			}
			continue
		}
		src := source
		if strings.HasPrefix(f.name, "actual_") {
			src = SourceObserved
		}
		switch f.kind {
		case kMoney:
			a, code := money.ParseAmount(rv)
			if code != "" {
				add(f.name, code, moneyMsg(f.name, code))
				continue
			}
			setAmount(&p, f.name, a)
		case kPct:
			v, code := money.ParsePct(rv)
			if code != "" {
				add(f.name, code, pctMsg(f.name, code))
				continue
			}
			setPct(&p, f.name, v)
		default:
			var s string
			if err := json.Unmarshal(rv, &s); err != nil {
				add(f.name, CodeNotText, f.name+" must be a string")
				continue
			}
			if code, msg := checkText(f, s); code != "" {
				add(f.name, code, msg)
				continue
			}
			setText(&p, f.name, s)
		}
		p.Sources[f.name] = src
	}
	if len(errs) > 0 {
		return Pilot{}, errs
	}
	p.BenefitType = CanonicalBenefitType(p.BenefitTypeRaw)
	if p.OverlapGroup == nil {
		p.Sources["overlap_group"] = SourceUnknown
	}
	return p, nil
}

func checkText(f fieldSpec, s string) (string, string) {
	t := strings.TrimSpace(s)
	switch f.kind {
	case kID:
		if !idRE.MatchString(t) {
			return CodeBadID, "id must be 1-64 characters: letters, digits, _ or -"
		}
	case kText:
		if t == "" {
			return CodeEmpty, f.name + " must not be empty"
		}
		if len(t) > 500 {
			return CodeTooLong, f.name + " must be at most 500 characters"
		}
	case kLabel:
		if len(t) > 100 {
			return CodeTooLong, f.name + " must be at most 100 characters"
		}
		if f.name == "overlap_group" && t != "" && !labelRE.MatchString(NormalizeLabel(t)) {
			return CodeBadLabel, "overlap_group must be 1-64 characters: letters, digits, space, _ . & -"
		}
	case kReadiness:
		if !ReadinessLabels[NormalizeLabel(t)] {
			return CodeInvalidEnum, "readiness_label must be one of low, medium, high"
		}
	}
	return "", ""
}

func moneyMsg(f, code string) string {
	switch code {
	case money.CodeNegative:
		return f + " must be >= 0"
	case money.CodePrecision:
		return f + " must have at most 2 decimal places"
	case money.CodeTooLarge:
		return f + " is unrealistically large"
	}
	return f + ` must be a JSON number in INR (strings such as "2 lakh" are rejected)`
}

func pctMsg(f, code string) string {
	switch code {
	case money.CodeOutOfRange:
		return f + " must be between 0 and 100"
	case money.CodePrecision:
		return f + " must have at most 2 decimal places"
	}
	return f + " must be a JSON number between 0 and 100"
}

func setAmount(p *Pilot, name string, a money.Amount) {
	switch name {
	case "setup_cost_inr":
		p.SetupCost = a
	case "annual_running_cost_inr":
		p.RunningCost = a
	case "annual_benefit_inr":
		p.AnnualBenefit = a
	case "capacity_value_inr":
		p.CapacityValue = a
	case "realized_cash_saving_inr":
		p.RealizedCashSaving = a
	}
}

func setPct(p *Pilot, name string, v money.Pct) {
	switch name {
	case "expected_adoption_pct":
		p.ExpectedAdoption = v
	case "actual_adoption_pct":
		p.ActualAdoption = v
	case "minimum_adoption_pct":
		p.MinimumAdoption = v
	case "expected_quality_pct":
		p.ExpectedQuality = v
	case "actual_quality_pct":
		p.ActualQuality = v
	case "minimum_quality_pct":
		p.MinimumQuality = v
	case "data_readiness_pct":
		p.DataReadiness = v
	case "technical_readiness_pct":
		p.TechnicalReadiness = v
	case "process_readiness_pct":
		p.ProcessReadiness = v
	}
}

func setText(p *Pilot, name, s string) {
	t := strings.TrimSpace(s)
	switch name {
	case "id":
		p.ID = t
	case "task":
		p.Task = t
	case "description":
		p.Description = &t
	case "benefit_type":
		raw := s
		p.BenefitTypeRaw = &raw
	case "overlap_group":
		raw := s
		p.OverlapGroupRaw = &raw
		if n := NormalizeLabel(s); n != "" {
			p.OverlapGroup = &n
		}
	case "readiness_label":
		n := NormalizeLabel(s)
		p.ReadinessLabel = &n
	}
}

// ApplyOverrides returns a copy of base with the given fields replaced (validated exactly like normal input).
// Overridden values are labelled `source` (actual_* stay OBSERVED). id cannot be overridden. base is not modified.
func ApplyOverrides(base Pilot, overrides json.RawMessage, source string) (Pilot, []string, []FieldError) {
	obj, errs := decodeObject(overrides)
	if errs != nil {
		return Pilot{}, nil, errs
	}
	if _, ok := obj["id"]; ok {
		return Pilot{}, nil, []FieldError{{Field: "overrides.id", Code: CodeUnknownField, Message: "id cannot be overridden"}}
	}
	full := map[string]json.RawMessage{"id": mustJSON(base.ID), "task": mustJSON(base.Task)}
	for k, v := range obj {
		full[k] = v
	}
	parsed, perrs := BuildPilot(full, source)
	if perrs != nil {
		for i := range perrs {
			perrs[i].Field = "overrides." + perrs[i].Field
		}
		return Pilot{}, nil, perrs
	}
	out := base.Clone()
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		copyField(&out, parsed, k)
		out.Sources[k] = parsed.Sources[k]
	}
	return out, keys, nil
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func copyField(dst *Pilot, src Pilot, name string) {
	switch name {
	case "task":
		dst.Task = src.Task
	case "description":
		dst.Description = src.Description
	case "setup_cost_inr":
		dst.SetupCost = src.SetupCost
	case "annual_running_cost_inr":
		dst.RunningCost = src.RunningCost
	case "annual_benefit_inr":
		dst.AnnualBenefit = src.AnnualBenefit
	case "capacity_value_inr":
		dst.CapacityValue = src.CapacityValue
	case "realized_cash_saving_inr":
		dst.RealizedCashSaving = src.RealizedCashSaving
	case "benefit_type":
		dst.BenefitTypeRaw, dst.BenefitType = src.BenefitTypeRaw, src.BenefitType
	case "overlap_group":
		dst.OverlapGroupRaw, dst.OverlapGroup = src.OverlapGroupRaw, src.OverlapGroup
	case "readiness_label":
		dst.ReadinessLabel = src.ReadinessLabel
	default:
		var v money.Pct
		switch name {
		case "expected_adoption_pct":
			v = src.ExpectedAdoption
		case "actual_adoption_pct":
			v = src.ActualAdoption
		case "minimum_adoption_pct":
			v = src.MinimumAdoption
		case "expected_quality_pct":
			v = src.ExpectedQuality
		case "actual_quality_pct":
			v = src.ActualQuality
		case "minimum_quality_pct":
			v = src.MinimumQuality
		case "data_readiness_pct":
			v = src.DataReadiness
		case "technical_readiness_pct":
			v = src.TechnicalReadiness
		case "process_readiness_pct":
			v = src.ProcessReadiness
		}
		setPct(dst, name, v)
	}
}

// MergeObjects overlays extension fields onto a fixture object. Fixture values win; conflicts are reported.
// Extension aliases are renamed. Official money fields cannot be supplied by an extension.
func MergeObjects(fixture, ext map[string]json.RawMessage) (map[string]json.RawMessage, []string, []FieldError) {
	out := make(map[string]json.RawMessage, len(fixture)+len(ext))
	for k, v := range fixture {
		out[k] = v
	}
	var warnings []string
	var errs []FieldError
	keys := make([]string, 0, len(ext))
	for k := range ext {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "id" {
			continue
		}
		name := k
		if a, ok := ExtensionAliases[k]; ok {
			name = a
		}
		switch name {
		case "task", "setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr", "benefit_type", "overlap_group":
			errs = append(errs, FieldError{Field: "extension." + k, Code: CodeUnknownField,
				Message: "official fixture fields cannot be supplied by the team extension"})
			continue
		}
		if _, ok := specByName[name]; !ok {
			errs = append(errs, FieldError{Field: "extension." + k, Code: CodeUnknownField, Message: fmt.Sprintf("field %q is not part of the extension schema", k)})
			continue
		}
		if cur, ok := out[name]; ok && string(bytes.TrimSpace(cur)) != "null" {
			if string(bytes.TrimSpace(ext[k])) != "null" && !bytes.Equal(bytes.TrimSpace(cur), bytes.TrimSpace(ext[k])) {
				warnings = append(warnings, fmt.Sprintf("EXTENSION_CONFLICT: %s fixture value kept", name))
			}
			continue
		}
		out[name] = ext[k]
	}
	return out, warnings, errs
}
