package money

import (
	"encoding/json"
	"math/big"
	"testing"
)

func TestParseAmount(t *testing.T) {
	ok := map[string]int64{"0": 0, "200000": 20000000, "0.5": 50, "1e5": 10000000, " 12.34 ": 1234, "-0": 0}
	for in, paise := range ok {
		a, code := ParseAmount(json.RawMessage(in))
		if code != "" || !a.Known || a.Paise != paise {
			t.Errorf("%q: %+v %s", in, a, code)
		}
	}
	bad := map[string]string{`"2 lakh"`: CodeNotNumeric, `"200000"`: CodeNotNumeric, "true": CodeNotNumeric, "{}": CodeNotNumeric,
		"[1]": CodeNotNumeric, "-1": CodeNegative, "0.001": CodePrecision, "1e14": CodeTooLarge, "1.": CodeNotNumeric,
		"+1": CodeNotNumeric, "01": CodeNotNumeric, ".5": CodeNotNumeric, "NaN": CodeNotNumeric, "Infinity": CodeNotNumeric,
		"99999999999999999999999999999999999999999999": CodeTooLarge}
	for in, want := range bad {
		if _, code := ParseAmount(json.RawMessage(in)); code != want {
			t.Errorf("%q: got %s want %s", in, code, want)
		}
	}
	for _, in := range []string{"", "null", "  null "} {
		if a, code := ParseAmount(json.RawMessage(in)); code != "" || a.Known {
			t.Errorf("%q must be unknown (never zero)", in)
		}
	}
}

func TestParsePct(t *testing.T) {
	if p, c := ParsePct(json.RawMessage("33.33")); c != "" || p.Centi != 3333 {
		t.Fatal(p, c)
	}
	for in, want := range map[string]string{"100.01": CodeOutOfRange, "-0.01": CodeOutOfRange, "50.001": CodePrecision, `"50"`: CodeNotNumeric} {
		if _, c := ParsePct(json.RawMessage(in)); c != want {
			t.Errorf("%s: %s", in, c)
		}
	}
}

func TestJSONRoundTripAndFormatting(t *testing.T) {
	type doc struct {
		A Amount `json:"a"`
		U Amount `json:"u"`
		P Pct    `json:"p"`
		Q Pct    `json:"q"`
		D Dec    `json:"d"`
		N Dec    `json:"n"`
	}
	in := doc{A: Amount{Paise: 12345, Known: true}, P: Percent(70), D: Dec{R: big.NewRat(-1, 3)}}
	b, _ := json.Marshal(in)
	if string(b) != `{"a":123.45,"u":null,"p":70,"q":null,"d":-0.33,"n":null}` {
		t.Fatal(string(b))
	}
	var out doc
	if err := json.Unmarshal(b, &out); err != nil || out.A != in.A || out.U.Known || out.P != in.P || out.N.Known() {
		t.Fatal(err, out)
	}
	if err := json.Unmarshal([]byte(`{"a":-1}`), &out); err == nil {
		t.Fatal("negative must fail unmarshal")
	}
	if err := json.Unmarshal([]byte(`{"p":101}`), &out); err == nil {
		t.Fatal("pct > 100 must fail unmarshal")
	}
	if err := json.Unmarshal([]byte(`{"d":"x"}`), &out); err == nil {
		t.Fatal("dec string must fail")
	}
	if Unknown().String() != "unknown" || (Pct{}).String() != "unknown" || (Dec{}).String() != "unknown" || Rupees(5).String() != "5" ||
		Percent(5).String() != "5" || FormatRat(big.NewRat(-1, 1000), 2) != "0" {
		t.Fatal("formatting")
	}
	if f, ok := (Dec{R: big.NewRat(1, 4)}).Float(); !ok || f != 0.25 {
		t.Fatal("float")
	}
	if _, ok := (Dec{}).Float(); ok {
		t.Fatal("unknown float")
	}
}

func TestRatPanicsOnUnknown(t *testing.T) {
	for _, f := range []func(){func() { Unknown().Rat() }, func() { (Pct{}).Rat() }} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("using an unknown value as a number must panic, never silently become 0")
				}
			}()
			f()
		}()
	}
}
