# AI investment decision

> Deterministic engine output. Inputs marked assumed are team assumptions, not measurements.

Rules version 9 · formula cf-1 · mode independent · horizon 36 months · r = 0.1 · budget INR 700,000

## Recommendation
**Run A as a limited pilot**

Run A as a limited pilot. Funded set B, A: INR 370,000/yr net savings on INR 700,000 invested.

- A ranks first with score 79.04.
- B recommended: highest net benefit within budget (2.5 L/yr net annual savings, break-even 24.0 months). Funded together with A: total INR 370,000/yr net savings. Horizon 36 months at r = 0.1: ROI and break-even are supporting evidence, not the selector. C dropped: 3-year loss (break-even beyond the horizon).

## Key numbers

| KPI | Value | Source |
|---|---|---|
| Total investment | INR 700,000 | Σ setup of the funded set |
| Net annual savings | INR 370,000 | Σ net annual benefit of the funded set (compare selection) |
| Cashable savings | INR 580,000 | Σ gross × cashable share |
| Capacity value | INR 0 | Σ gross − cashable |
| 36-month profit | INR 410,000 | Σ multi-period profit of the funded set |
| NPV | INR 267,120 | Σ NPV at r = 0.1 |
| 36-month ROI | 58.6 % | portfolio profit ÷ total investment |
| Portfolio break-even | 23 month | first month the funded set's summed cumulative cash flow is ≥ 0 |
| AI burn to date | INR 1,550,000 | observed (confirmed balance-sheet rows) |
| Next-FY AI cost | INR 182,000 | assumed growth fallback (send usage history to /forecast/usage for a backtested forecast) |

## Proposals

| Proposal | State | Outcome | Score | Net/yr | Payback | ROI | Break-even | Evidence | Budget |
|---|---|---|---|---|---|---|---|---|---|
| B | recommended | gather_evidence | 73.5 | INR 250,000 | 24 mo | 50% | month 24 | low | within |
| A | recommended | limited_pilot | 79.04 | INR 120,000 | 20 mo | 80% | month 20 | low | within |
| C | not_viable | renegotiate | 10 | INR 20,000 | 60 mo | -40% | beyond horizon | low | within |

## Yearly savings (funded set)

| Year | Benefit | Running cost | One-time | Net | Cumulative |
|---|---|---|---|---|---|
| 1 | INR 580,000 | INR 210,000 | INR 700,000 | INR -330,000 | INR -330,000 |
| 2 | INR 580,000 | INR 210,000 | INR 0 | INR 370,000 | INR 40,000 |
| 3 | INR 580,000 | INR 210,000 | INR 0 | INR 370,000 | INR 410,000 |

## Risks
- **high** GATE_BREAKEVEN_IN_HORIZON: fails the BREAKEVEN_IN_HORIZON gate (C)
- **low** BASELINE_MISSING: No baseline (today's cost of the process); retained human cost is unknown. (B, C)
- **low** OVERLAP_UNKNOWN: Overlap group unknown; treated as standalone. Confirm with owner. (C)

## Next actions
- **MONEY_ASSUMED**: back one money figure with an invoice, a signed contract or a written vendor quote (owner hint: finance; expected test: a decision-driving money input sourced as observed)
- **NO_OBSERVED_ADOPTION**: run one adoption spot-check batch over the real users (owner hint: org lead; expected test: observed adoption recorded from the batch)
- **BASELINE_MISSING**: record today's cost of the process (volume, minutes per unit, loaded hourly cost) (owner hint: ops lead; expected test: baseline cost known; retained human cost computed)
- **NO_OBSERVED_QUALITY**: run one quality spot-check on pilot outputs and record it (owner hint: ops lead; expected test: observed quality recorded from the batch)

## Consistency checks
- ✓ yearly net totals equal multi-period profit for every proposal
- ✓ KPI net annual savings equals Σ funded compare rows
- ✓ KPI profit equals Σ funded yearly net
- ✓ portfolio cumulative series ends at the KPI profit
