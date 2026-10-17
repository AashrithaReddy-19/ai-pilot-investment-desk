// Package money holds exact, nullable INR amounts and percentages.
//
// Amounts are int64 paise (1 INR = 100 paise); derived values use math/big.Rat.
// No float64 ever touches money, so 200000*12/120000 is exactly 20.
// "Unknown" (JSON null) is a distinct state from zero and is never converted to zero.
package money

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strings"
)

// Validation codes returned by the parsers. Shared with the public API error contract.
const (
	CodeNotNumeric = "NOT_NUMERIC"    // strings ("2 lakh", "200000"), booleans, objects
	CodeNegative   = "NEGATIVE_VALUE" // money < 0
	CodeOutOfRange = "OUT_OF_RANGE"   // percent outside 0..100
	CodePrecision  = "TOO_PRECISE"    // more than 2 decimal places
	CodeTooLarge   = "TOO_LARGE"      // above MaxRupees
)

// MaxRupees bounds any single amount (INR 10 lakh crore). Keeps all products far inside int64/big ranges.
const MaxRupees = 10_000_000_000_000

// Amount is a nullable exact INR amount stored in paise.
type Amount struct {
	Paise int64
	Known bool
}

// Rupees builds a known amount from whole rupees.
func Rupees(r int64) Amount { return Amount{Paise: r * 100, Known: true} }

// Unknown is the explicit null amount.
func Unknown() Amount { return Amount{} }

// Rat returns the amount in rupees as an exact rational. Panics if unknown: callers must check Known.
func (a Amount) Rat() *big.Rat {
	if !a.Known {
		panic("money: Rat() on unknown amount")
	}
	return new(big.Rat).SetFrac64(a.Paise, 100)
}

// String renders rupees with up to 2 decimals, or "unknown".
func (a Amount) String() string {
	if !a.Known {
		return "unknown"
	}
	return FormatRat(a.Rat(), 2)
}

func (a Amount) MarshalJSON() ([]byte, error) {
	if !a.Known {
		return []byte("null"), nil
	}
	return []byte(FormatRat(a.Rat(), 2)), nil
}

func (a *Amount) UnmarshalJSON(b []byte) error {
	v, code := ParseAmount(b)
	if code != "" {
		return errors.New(code)
	}
	*a = v
	return nil
}

// ParseAmount parses a raw JSON value strictly. null/absent -> Unknown. Returns a validation code on failure.
func ParseAmount(raw json.RawMessage) (Amount, string) {
	r, code := parseNumber(raw)
	if code != "" || r == nil {
		return Amount{}, code
	}
	if r.Sign() < 0 {
		return Amount{}, CodeNegative
	}
	p := new(big.Rat).Mul(r, big.NewRat(100, 1))
	if !p.IsInt() {
		return Amount{}, CodePrecision
	}
	if r.Cmp(big.NewRat(MaxRupees, 1)) > 0 {
		return Amount{}, CodeTooLarge
	}
	return Amount{Paise: p.Num().Int64(), Known: true}, ""
}

// Pct is a nullable percentage 0..100 stored in hundredths of a percent (exact).
type Pct struct {
	Centi int64
	Known bool
}

func Percent(p int64) Pct { return Pct{Centi: p * 100, Known: true} }

func (p Pct) Rat() *big.Rat {
	if !p.Known {
		panic("money: Rat() on unknown pct")
	}
	return new(big.Rat).SetFrac64(p.Centi, 100)
}

func (p Pct) String() string {
	if !p.Known {
		return "unknown"
	}
	return FormatRat(p.Rat(), 2)
}

func (p Pct) MarshalJSON() ([]byte, error) {
	if !p.Known {
		return []byte("null"), nil
	}
	return []byte(FormatRat(p.Rat(), 2)), nil
}

func (p *Pct) UnmarshalJSON(b []byte) error {
	v, code := ParsePct(b)
	if code != "" {
		return errors.New(code)
	}
	*p = v
	return nil
}

// ParsePct parses a percentage strictly (0..100, max 2 decimals).
func ParsePct(raw json.RawMessage) (Pct, string) {
	r, code := parseNumber(raw)
	if code != "" || r == nil {
		return Pct{}, code
	}
	if r.Sign() < 0 || r.Cmp(big.NewRat(100, 1)) > 0 {
		return Pct{}, CodeOutOfRange
	}
	c := new(big.Rat).Mul(r, big.NewRat(100, 1))
	if !c.IsInt() {
		return Pct{}, CodePrecision
	}
	return Pct{Centi: c.Num().Int64(), Known: true}, ""
}

var jsonNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// parseNumber returns (nil, "") for null/empty, the exact value for a JSON number, or a code.
// JSON has no NaN/Infinity literals, so those are already rejected by the JSON decoder as malformed input.
func parseNumber(raw json.RawMessage) (*big.Rat, string) {
	s := bytes.TrimSpace(raw)
	if len(s) == 0 || string(s) == "null" {
		return nil, ""
	}
	if len(s) > 40 {
		if jsonNumber.Match(s) {
			return nil, CodeTooLarge
		}
		return nil, CodeNotNumeric
	}
	if !jsonNumber.Match(s) { // strict JSON number grammar: rejects "2 lakh", "1.", "+1", "01", strings, booleans
		return nil, CodeNotNumeric
	}
	r, ok := new(big.Rat).SetString(string(s))
	if !ok {
		return nil, CodeNotNumeric
	}
	return r, ""
}

// FormatRat renders r rounded half away from zero to `places` decimals, trimming trailing zeros.
func FormatRat(r *big.Rat, places int) string {
	s := r.FloatString(places)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		return "0"
	}
	return s
}

// Dec is a nullable exact decimal for derived metrics (payback months, ROI...). JSON: number or null.
type Dec struct {
	R *big.Rat
}

func (d Dec) Known() bool { return d.R != nil }

func (d Dec) MarshalJSON() ([]byte, error) {
	if d.R == nil {
		return []byte("null"), nil
	}
	return []byte(FormatRat(d.R, 2)), nil
}

func (d *Dec) UnmarshalJSON(b []byte) error {
	r, code := parseNumber(b)
	if code != "" {
		return errors.New(code)
	}
	d.R = r
	return nil
}

func (d Dec) String() string {
	if d.R == nil {
		return "unknown"
	}
	return FormatRat(d.R, 2)
}

// Float is for non-financial uses only (sorting keys, logs). Never use it for money arithmetic.
func (d Dec) Float() (float64, bool) {
	if d.R == nil {
		return 0, false
	}
	f, _ := d.R.Float64()
	return f, true
}
