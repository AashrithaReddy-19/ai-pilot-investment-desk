package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strings"

	"c23desk/internal/decision"
	"c23desk/internal/money"
	"c23desk/internal/rules"
)

// ExportDoc is the JSON evidence export of a portfolio.
type ExportDoc struct {
	Disclaimer       string                         `json:"disclaimer"`
	RulesVersion     string                         `json:"rules_version"`
	Portfolio        PortfolioView                  `json:"portfolio"`
	PilotEvaluations map[string]decision.Evaluation `json:"pilot_evaluations"`
}

// Disclaimer is attached to every export. The fixture is synthetic; no ROI claim is real.
const Disclaimer = "Synthetic C23 teaching fixture plus team assumptions. Figures are calculations on stated assumptions, " +
	"not proven enterprise ROI or validated market performance. AI outputs, if any, are advisory only."

// Export builds the evidence document.
func (s *Service) Export(ctx context.Context, id string) (ExportDoc, error) {
	v, err := s.GetPortfolio(ctx, id)
	if err != nil {
		return ExportDoc{}, err
	}
	ps, err := s.Store.ListPilots(ctx)
	if err != nil {
		return ExportDoc{}, fromStore(err, "pilots")
	}
	all := s.evaluateAll(ps)
	ev := map[string]decision.Evaluation{}
	for _, pid := range v.PilotIDs {
		if e, ok := all[pid]; ok {
			ev[pid] = e
		}
	}
	v.Cache = ""
	return ExportDoc{Disclaimer: Disclaimer, RulesVersion: rules.Version, Portfolio: v, PilotEvaluations: ev}, nil
}

// LedgerCSV renders the benefit attribution ledger.
func LedgerCSV(d ExportDoc) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"pilot_id", "benefit_group", "claimed_benefit_inr", "basis_benefit_inr", "recognized_benefit_inr",
		"annual_running_cost_inr", "source", "resolution", "review_status", "review_note", "decision"})
	for _, l := range d.Portfolio.Assessment.Ledger {
		g := ""
		if l.BenefitGroup != nil {
			g = *l.BenefitGroup
		}
		_ = w.Write([]string{l.PilotID, g, csvAmt(l.ClaimedBenefit), csvAmt(l.BasisBenefit), csvAmt(l.RecognizedBenefit),
			csvAmt(l.RunningCost), l.Source, l.Resolution, l.ReviewStatus, sanitizeCSV(l.ReviewNote), d.PilotEvaluations[l.PilotID].Decision})
	}
	w.Flush()
	return b.Bytes()
}

func csvAmt(a money.Amount) string {
	if !a.Known {
		return ""
	}
	return a.String()
}

// sanitizeCSV neutralises spreadsheet formula injection in free-text cells.
func sanitizeCSV(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// mdCell keeps free text inside one Markdown table cell (no row/column injection).
func mdCell(s string) string {
	return strings.NewReplacer("|", "/", "\r", " ", "\n", " ", "<", "&lt;", ">", "&gt;").Replace(s)
}

// EvidenceMarkdown renders a human-readable evidence report.
func EvidenceMarkdown(d ExportDoc) []byte {
	a := d.Portfolio.Assessment
	var b strings.Builder
	fmt.Fprintf(&b, "# C23 Portfolio Evidence: %s\n\n> %s\n\n", d.Portfolio.Name, d.Disclaimer)
	fmt.Fprintf(&b, "- Rules version: `%s`\n- Status: **%s** (approval allowed: %t)\n- Basis: %s\n- Budget: INR %s\n",
		d.RulesVersion, d.Portfolio.Status, a.ApprovalAllowed, a.Basis, a.Budget)
	fmt.Fprintf(&b, "- Total setup: INR %s; budget remaining: INR %s\n- Recognised annual benefit: INR %s (claimed INR %s)\n",
		a.TotalSetup, a.BudgetRemaining, a.TotalRecognized, a.TotalClaimed)
	fmt.Fprintf(&b, "- Annual running cost: INR %s; net annual benefit: INR %s; payback: %s months (%s)\n\n",
		a.TotalRunning, a.NetAnnual, a.Payback.Months, a.Payback.Status)
	b.WriteString("## Benefit attribution ledger\n\n| Pilot | Group | Claimed | Recognised | Resolution | Review | Note |\n|---|---|---|---|---|---|---|\n")
	for _, l := range a.Ledger {
		g := "unknown"
		if l.BenefitGroup != nil {
			g = *l.BenefitGroup
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n", l.PilotID, g, l.ClaimedBenefit, l.RecognizedBenefit, l.Resolution, l.ReviewStatus,
			mdCell(l.ReviewNote))
	}
	b.WriteString("\n## Blockers\n\n")
	if len(a.Blockers) == 0 {
		b.WriteString("- none\n")
	}
	for _, x := range a.Blockers {
		fmt.Fprintf(&b, "- `%s` %s\n", x.Code, x.Message)
	}
	b.WriteString("\n## Pilot decisions (deterministic, c23-rules-v1)\n\n")
	for _, pid := range a.PilotIDs {
		e := d.PilotEvaluations[pid]
		fmt.Fprintf(&b, "- **%s: %s** (%s). Reasons: %s\n", pid, e.Decision, e.PrimaryReason, strings.Join(e.ReasonCodes, ", "))
	}
	return []byte(b.String())
}
