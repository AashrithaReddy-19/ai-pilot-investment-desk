package docparse

import (
	"regexp"
	"strings"
	"unicode"
)

// This file recognises money as a document prints it. It never converts a figure: the text is copied
// through unchanged (apart from the minus sign and currency marker, below) and the engine does the maths.

// Units the engine understands, and the ones it does not.
const (
	unitRupees    = "rupees"
	unitThousands = "thousands"
	unitLakh      = "lakh"
	unitCrore     = "crore"
	unitMillions  = "millions" // not supported by the engine
	unitBillions  = "billions" // not supported by the engine
)

type amountInfo struct {
	OK       bool
	Blank    bool   // a dash or "n/a": a missing amount, never zero
	Currency string // "INR", "USD", ... or "" when the cell names none
	Suffix   string // trailing cr / l / k / dr / ... exactly as printed
	Number   string // the digits (with grouping) as printed, sign excluded
	Negative bool
	Percent  bool
}

var currencyTokens = []struct{ token, code string }{
	{"₹", "INR"}, {"rs.", "INR"}, {"rs", "INR"}, {"inr", "INR"},
	{"us$", "USD"}, {"usd", "USD"}, {"$", "USD"},
	{"€", "EUR"}, {"eur", "EUR"},
	{"£", "GBP"}, {"gbp", "GBP"},
}

var suffixRe = regexp.MustCompile(`(?i)^(crores?|crs?\.?|lakhs?|lacs?|l|k|mn|bn|dr)$`)
var numberRe = regexp.MustCompile(`^\d[\d,]*(\.\d+)?$|^\.\d+$`)

// normSpaces turns the many Unicode spaces and the Unicode minus into plain ones.
func normSpaces(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '−': // MINUS SIGN
			return '-'
		case r == ' ' || r == ' ' || r == ' ' || r == ' ' || r == '​':
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// parseAmount reads a cell that is, or is not, an amount. It is deliberately strict: text that merely
// contains digits ("FY 2025-26", "Note 3") is not an amount.
func parseAmount(raw string) amountInfo {
	s := normSpaces(raw)
	if s == "" {
		return amountInfo{}
	}
	switch strings.ToLower(s) {
	case "-", "--", "—", "–", "n/a", "na", "nil", "n.a.", "…":
		return amountInfo{OK: true, Blank: true}
	}
	var a amountInfo
	bracketed := false
	// leading currency, sign and brackets, in any order a statement uses
	for changed := true; changed; {
		changed = false
		low := strings.ToLower(s)
		for _, ct := range currencyTokens {
			if a.Currency == "" && strings.HasPrefix(low, ct.token) {
				a.Currency, s, changed = ct.code, strings.TrimSpace(s[len(ct.token):]), true
				break
			}
		}
		switch {
		case strings.HasPrefix(s, "(") && !bracketed:
			a.Negative, bracketed, s, changed = true, true, strings.TrimSpace(s[1:]), true
		case strings.HasPrefix(s, "-"), strings.HasPrefix(s, "–"), strings.HasPrefix(s, "+"):
			if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "–") {
				a.Negative = true
			}
			s, changed = strings.TrimSpace(s[1:]), true
		}
	}
	// brackets must match: "(120.00)" is a negative amount, but "2)" (the tail of "(segment 2)") and an
	// unclosed "(120.00" are not amounts
	s = strings.TrimSpace(s)
	if bracketed {
		if !strings.HasSuffix(s, ")") {
			return amountInfo{}
		}
		s = strings.TrimSpace(strings.TrimSuffix(s, ")"))
	} else if strings.HasSuffix(s, ")") {
		return amountInfo{}
	}
	if strings.HasSuffix(s, "%") {
		a.Percent, s = true, strings.TrimSpace(strings.TrimSuffix(s, "%"))
	}
	// trailing currency or suffix: "250 INR", "2.5 cr", "1,20,000 Dr"
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return amountInfo{}
	}
	if len(fields) == 2 {
		last := strings.ToLower(fields[1])
		for _, ct := range currencyTokens {
			if a.Currency == "" && last == ct.token {
				a.Currency, fields = ct.code, fields[:1]
				break
			}
		}
	}
	if len(fields) == 2 && suffixRe.MatchString(fields[1]) {
		a.Suffix, fields = fields[1], fields[:1]
	}
	if len(fields) != 1 {
		return amountInfo{}
	}
	core := fields[0]
	// "2.5cr", "50k": a suffix glued to the number
	if a.Suffix == "" {
		for i := len(core); i > 0; i-- {
			if core[i-1] >= '0' && core[i-1] <= '9' || core[i-1] == '.' {
				if i < len(core) && suffixRe.MatchString(core[i:]) {
					a.Suffix, core = core[i:], core[:i]
				}
				break
			}
		}
	}
	if !numberRe.MatchString(core) {
		return amountInfo{}
	}
	a.OK, a.Number = true, core
	return a
}

// yearLike is a bare four-digit year: a column heading, not an amount.
func yearLike(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 4 {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s[:2] == "19" || s[:2] == "20"
}

// hasLetters reports whether s has at least n letters.
func hasLetters(s string, n int) bool {
	c := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			if c++; c >= n {
				return true
			}
		}
	}
	return false
}

// ---- units ----

const unitContext = `(?:\bin\b|₹|\brs\b\.?|\binr\b|\(|\bamounts?\b|\bfigures?\b|\bunits?\b|\bexpressed\b|\bstated\b)`

var unitRes = []struct {
	unit string
	re   *regexp.Regexp
}{
	{unitCrore, regexp.MustCompile(`(?i)` + unitContext + `[^.;:\n]{0,30}?\b(?:crores?|crs?)\b`)},
	{unitLakh, regexp.MustCompile(`(?i)` + unitContext + `[^.;:\n]{0,30}?\b(?:lakhs?|lacs?)\b`)},
	{unitThousands, regexp.MustCompile(`(?i)` + unitContext + `[^.;:\n]{0,30}?(?:\bthousands?\b|['’]000s?\b|\(000s?\)|\b000s?\b)`)},
	{unitMillions, regexp.MustCompile(`(?i)` + unitContext + `[^.;:\n]{0,30}?\b(?:millions?|mn|mm)\b`)},
	{unitBillions, regexp.MustCompile(`(?i)` + unitContext + `[^.;:\n]{0,30}?\b(?:billions?|bn)\b`)},
	{unitRupees, regexp.MustCompile(`(?i)\b(?:in|amounts?\s+in|figures?\s+in|stated\s+in|expressed\s+in)\s+(?:indian\s+)?(?:rupees|inr|rs\.?)\b(?:\s+(?:only|actuals?))?`)},
}

// unitsIn lists the units a piece of text states, in a stable order. It is meant for titles and column
// headings, never for the descriptions of individual lines.
func unitsIn(text string) []string {
	var out []string
	scaled := false
	for _, u := range unitRes {
		if u.unit == unitRupees && scaled {
			continue // "in INR lakh" states lakh; the "in INR" in it is not a second, separate unit
		}
		if u.re.MatchString(text) {
			out = append(out, u.unit)
			scaled = scaled || u.unit != unitRupees
		}
	}
	return out
}

// hasUnitWord reports whether a column heading already states a unit the engine would pick up.
func hasUnitWord(header string) bool {
	return len(unitsIn(header)) > 0
}

// currencyIn finds a currency the text names, e.g. "(USD in millions)" or "$ in thousands".
var currencyWordRe = regexp.MustCompile(`(?i)(?:\bus\$|\busd\b|\$|€|\beur\b|£|\bgbp\b|\bus dollars?\b|\beuros?\b|\bpounds?\b)`)
var inrWordRe = regexp.MustCompile(`(?i)(?:₹|\brs\b\.?|\binr\b|\brupees?\b)`)

func currencyIn(text string) (code string) {
	if m := currencyWordRe.FindString(text); m != "" {
		switch strings.ToLower(m) {
		case "€", "eur", "euro", "euros":
			return "EUR"
		case "£", "gbp", "pound", "pounds":
			return "GBP"
		}
		return "USD"
	}
	if inrWordRe.MatchString(text) {
		return "INR"
	}
	return ""
}

// cleanAmountText is the one change made to a figure on its way to the engine: the currency marker and
// the Unicode minus are normalised (the engine reads "₹ 250" but not "Rs. 250"), and a bracketed negative
// is written as a minus. Digits, grouping, decimals and any cr/L/k suffix are kept exactly as printed.
func cleanAmountText(raw string, a amountInfo) string {
	if !a.OK {
		return strings.Join(strings.Fields(normSpaces(raw)), " ") // never a line break inside a cell
	}
	if a.Blank {
		return ""
	}
	out := a.Number
	if a.Suffix != "" {
		out += " " + a.Suffix
	}
	if a.Percent {
		out += "%"
	}
	if a.Negative {
		out = "-" + out
	}
	return out
}
