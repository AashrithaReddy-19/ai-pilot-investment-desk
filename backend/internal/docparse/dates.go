package docparse

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Excel stores dates as serial numbers; a heading like 31-Mar-2026 must not reach the reviewer as 46112.

var (
	quotedRe = regexp.MustCompile(`"[^"]*"`)
	escRe    = regexp.MustCompile(`\\.`)
	bracedRe = regexp.MustCompile(`\[[^\]]*\]`)
)

// builtinDate lists the built-in number-format ids that are dates or times.
func builtinDate(id int) bool {
	switch {
	case id >= 14 && id <= 22, id >= 27 && id <= 36, id >= 45 && id <= 47, id >= 50 && id <= 58:
		return true
	}
	return false
}

// isDateFormat reports whether a custom number-format code formats a date or time.
func isDateFormat(code string) bool {
	c := strings.ToLower(code)
	if c == "" || c == "general" || c == "@" {
		return false
	}
	c = quotedRe.ReplaceAllString(c, "")
	c = escRe.ReplaceAllString(c, "")
	c = bracedRe.ReplaceAllString(c, "")
	return strings.ContainsAny(c, "dmyhs")
}

// serialToText renders an Excel date serial as ISO text (date only when there is no time of day).
func serialToText(serial float64, date1904 bool) string {
	if math.IsNaN(serial) || math.IsInf(serial, 0) || serial < 0 || serial > 2958465 {
		return strconv.FormatFloat(serial, 'f', -1, 64)
	}
	var base time.Time
	switch {
	case date1904:
		base = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	case serial < 61: // before the phantom 1900-02-29
		base = time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
	default:
		base = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	}
	days := math.Floor(serial)
	secs := math.Round((serial - days) * 86400)
	t := base.AddDate(0, 0, int(days)).Add(time.Duration(secs) * time.Second)
	if secs == 0 {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02 15:04:05")
}

// numberText prints a stored number without losing a digit: no rounding, no scientific notation.
func numberText(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// xmlNumberText keeps a number exactly as the workbook XML stores it, expanding only exponent notation.
func xmlNumberText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.ContainsAny(s, "eE") {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return numberText(v)
		}
	}
	return strings.TrimPrefix(s, "+")
}
