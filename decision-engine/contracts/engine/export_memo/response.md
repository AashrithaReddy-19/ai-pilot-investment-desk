# Decision memo — AI1 (Invoice field extraction)

> Synthetic data. Inputs are assumptions. No claim of live revenue or production readiness.

## Recommendation
- Decision: **STAGE** — Looks payable (net INR 120,000/yr, break-even month 20) but the evidence band is Low: The band is capped by fixture (synthetic) values. Too thin to commit the full amount.
- Stop rule: **GO** — Adoption and quality meet thresholds.
- Checkpoint: **EXTEND** — Gates pass but the evidence band is low (band capped by fixture (synthetic) values; observed weight 3/11); run the plan longer before scaling.
- Money lens: independent mode; conservative net INR 66,000/yr.

## Key numbers (with provenance tags)

| Number | Value | Provenance tag | Detail |
|---|---|---|---|
| setup_cost_inr | INR 200,000 | fixture (synthetic) | setup_cost_inr from the fixture |
| annual_running_cost_inr | INR 60,000 | fixture (synthetic) | annual_running_cost_inr from the fixture |
| annual_benefit_inr | INR 180,000 | fixture (synthetic) | annual_benefit_inr from the fixture |
| net_annual_inr | INR 120,000 | calculated | = INR 180,000 − INR 60,000 = INR 120,000/yr |
| payback_months | INR 20 | calculated | = setup INR 200,000 ÷ net INR 120,000/yr × 12 |
| profit_inr | INR 160,000 | calculated | multi-period profit over the horizon |
| npv_inr | INR 113,447 | calculated | NPV at the request discount rate |
| adoption_pct | 70 | assumed (team) | team-authored extension |
| readiness | high | assumed (team) | team-authored extension |
| observed_adoption_pct | 72.0 | observed | recorded measurement |
| observed_quality_pct | 85.0 | observed | recorded measurement |
| breakeven_adoption_pct | 33.3% | calculated | adoption where net turns positive |

## Evidence band and gaps
- Band: **low** (observed weight 3/11; band capped by fixture (synthetic) values).
- Gap (high): annual_benefit_inr — The benefit is the biggest lever on net, payback and the ranking; without a driver or invoice trail the number is stated, not evidenced.
- Gap (medium): annual_running_cost_inr — The running cost is subtracted from every view; a wrong run cost flips net, payback and break-even adoption.
- Gap (medium): setup_cost_inr — The one-time cost sets payback and NPV; missing lines hide the real outlay.
- Gap (low): readiness — Readiness is a team judgement; scoring it against the data checklist would make the haircut evidence-backed.

## Next actions
- **FIXTURE_ONLY_EVIDENCE**: replace one fixture number with a real one: an invoice-based recurring cost line or a benefit driver from real volumes (owner hint: finance + service owner; expected test: no decision-driving input sourced from the fixture)

## 30-day plan
- No plan recorded yet — generate one at `POST /api/v1/plans`.

## Audit trail
- Pilot flags: none.
- Provenance tags present: assumed (team); calculated; fixture (synthetic); observed.
- Checkpoint inputs: adoption 72.0, quality 85.0, duration None days, revised before: False.
- Tie-break order: higher net benefit > faster payback > higher readiness > higher adoption > lower setup cost > lower id.
- Overrides and hypothesis rejections follow the policy; actuals come from observations, never invented.
