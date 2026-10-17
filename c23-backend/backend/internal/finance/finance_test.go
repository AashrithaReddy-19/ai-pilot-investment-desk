package finance

import (
	"encoding/json"
	"testing"

	"c23desk/internal/money"
)

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

// RELEASE-BLOCKING: official C23 reference case, computed dynamically (20 is never hard-coded in the engine).
func TestOfficialReference20MonthPayback(t *testing.T) {
	m := Compute(money.Rupees(200000), money.Rupees(60000), money.Rupees(180000))
	if js(m.NetAnnual) != "120000" || js(m.MonthlyNet) != "10000" || js(m.Months) != "20" || m.Status != PaybackCalculable {
		t.Fatalf("got net=%s monthly=%s payback=%s status=%s", js(m.NetAnnual), js(m.MonthlyNet), js(m.Months), m.Status)
	}
	if !m.Months.R.IsInt() || m.Months.R.Num().Int64() != 20 {
		t.Fatal("payback must be exactly 20, not approximately")
	}
	if js(m.ROIPct) != "60" {
		t.Fatalf("roi %s", js(m.ROIPct))
	}
}

func TestJudgeChangedRunningCost40Months(t *testing.T) {
	m := Compute(money.Rupees(200000), money.Rupees(120000), money.Rupees(180000))
	if js(m.NetAnnual) != "60000" || js(m.MonthlyNet) != "5000" || js(m.Months) != "40" {
		t.Fatalf("got %s %s %s", js(m.NetAnnual), js(m.MonthlyNet), js(m.Months))
	}
}

func TestNeverPaysBack(t *testing.T) {
	for _, run := range []int64{180000, 200000} {
		m := Compute(money.Rupees(200000), money.Rupees(run), money.Rupees(180000))
		if m.Status != PaybackNever || m.Months.Known() {
			t.Fatalf("run=%d: %+v", run, m)
		}
	}
	m := Compute(money.Rupees(200000), money.Rupees(200000), money.Rupees(180000))
	if js(m.NetAnnual) != "-20000" {
		t.Fatal(js(m.NetAnnual))
	}
}

func TestUnknownNeverZero(t *testing.T) {
	m := Compute(money.Rupees(200000), money.Unknown(), money.Rupees(180000))
	if m.NetAnnual.Known() || m.MonthlyNet.Known() || m.Months.Known() || m.ROIPct.Known() || m.Status != PaybackNotCalculable {
		t.Fatalf("unknown running cost must make outputs unknown: %+v", m)
	}
	if js(m.Months) != "null" || js(m.NetAnnual) != "null" {
		t.Fatal("unknown must serialise as null")
	}
	if Compute(money.Unknown(), money.Rupees(1), money.Rupees(2)).Status != PaybackNotCalculable {
		t.Fatal("unknown setup")
	}
}

func TestZeroSetup(t *testing.T) {
	m := Compute(money.Rupees(0), money.Rupees(60000), money.Rupees(180000))
	if m.Status != PaybackImmediate || js(m.Months) != "0" || m.ROIPct.Known() {
		t.Fatalf("%+v", m)
	}
}

func TestExactFractions(t *testing.T) {
	// 100000 / (70000/12) = 17.142857... -> rendered 17.14, held exactly internally
	m := Compute(money.Rupees(100000), money.Rupees(10000), money.Rupees(80000))
	if js(m.Months) != "17.14" || js(m.MonthlyNet) != "5833.33" {
		t.Fatalf("%s %s", js(m.Months), js(m.MonthlyNet))
	}
	// paise precision preserved: 0.1 + 0.2 style traps do not exist
	a, _ := money.ParseAmount(json.RawMessage("0.1"))
	b, _ := money.ParseAmount(json.RawMessage("0.2"))
	s, _ := SumAmounts(a, b)
	if s.String() != "0.3" {
		t.Fatal(s.String())
	}
}

func TestBreakevenAndBudget(t *testing.T) {
	if js(BreakevenAdoptionPct(money.Rupees(60000), money.Rupees(180000))) != "33.33" {
		t.Fatal("breakeven")
	}
	if BreakevenAdoptionPct(money.Rupees(1), money.Rupees(0)).Known() || BreakevenAdoptionPct(money.Unknown(), money.Rupees(1)).Known() {
		t.Fatal("breakeven unknown cases")
	}
	tot, ok := SumAmounts(money.Rupees(200000), money.Rupees(200000), money.Rupees(200000), money.Rupees(200000))
	if !ok || js(BudgetRemaining(money.Rupees(600000), tot)) != "-200000" {
		t.Fatal("budget remaining")
	}
	if _, ok := SumAmounts(money.Rupees(1), money.Unknown()); ok {
		t.Fatal("sum with unknown must be unknown")
	}
	if BudgetRemaining(money.Unknown(), tot).Known() {
		t.Fatal("unknown budget")
	}
}
