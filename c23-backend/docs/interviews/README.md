# Task-level interviews (MANUAL — not yet done)

> **Status: 0 of 3 completed.** No interview evidence exists in this repository. Nothing here may be presented as
> real-world validation until real notes are added below. Do not paraphrase from memory; write what was said.

## Who (pick 3 different roles)

1. Department / operations lead (owns the AI budget)
2. Finance or process owner (knows whether time saved becomes cash)
3. Support or team lead (knows adoption and quality in practice)

## Protocol (30 minutes, no pitch first)

Ask for consent to take notes; record role, company size band, date. Ask about **one real repetitive task**.

1. What repetitive task takes significant time?
2. How often does it happen? (per day / week)
3. How much time does it take each time?
4. What quality level is acceptable? How do you check it today?
5. What errors are unacceptable?
6. What would make you stop using an AI pilot?
7. What adoption level would make the pilot worthwhile?
8. Which recurring costs matter (licences, review time, IT)?
9. Is time saved actually removable cash cost, or only freed capacity?
10. Would a structured pilot investment review help you? Would you pay for it? How much?

## Capture template (copy once per interview into `interview-1.md`, `-2.md`, `-3.md`)

```
Interview n | date | role | company size band | consent: yes/no
Task:
Frequency:                Time per occurrence:
Acceptable quality:       Unacceptable errors:
Stop condition (verbatim):
Adoption that makes it worthwhile:
Recurring costs mentioned:
Time saved = cash? (yes/no/partly + why):
Would a review help? Would they pay? Price mentioned:
Verbatim quotes:
```

## Mapping to the product (fill after interviews)

| Evidence | Maps to | Current value | Label today | After interviews |
|---|---|---|---|---|
| adoption bar | `minimum_adoption_pct` (default 60%) | 60 | TEAM_ASSUMPTION | REAL INTERVIEW EVIDENCE if confirmed |
| quality bar / unacceptable errors | `minimum_quality_pct` (default 80%), STOP rule | 80 | TEAM_ASSUMPTION | |
| readiness concerns | readiness label / pct | per pilot | TEAM_ASSUMPTION | |
| stop condition | R3/R4 stop rules | STOP | TEAM_ASSUMPTION | |
| time vs cash | `benefit_type`, `realized_cash_saving_inr` | "cashable assumption" | OFFICIAL wording, unproven | |
| recurring costs | `annual_running_cost_inr` | fixture 60000 | OFFICIAL (synthetic) | |
| willingness to pay | unit_economics.csv price | 25000 | HYPOTHESIS | |
