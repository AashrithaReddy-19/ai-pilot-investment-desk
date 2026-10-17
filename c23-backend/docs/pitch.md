# Technical story (2 minutes)

**Problem.** Companies have many AI ideas but lack an auditable method for choosing which pilot deserves investment.
Vendors quote "time saved", benefits from different pilots overlap, recurring costs are missing, and nobody agrees in
advance when to stop.

**Solution.** The C23 AI Pilot Investment Decision Desk evaluates economics, adoption, quality, readiness, overlap and stop
conditions for each pilot and for a whole portfolio under a budget — and shows exactly why each decision was made.

**Architecture.** Any client → **Go + Gin** API (the only public entry point) → **PostgreSQL** (source of truth,
audit in the same transaction) + **Redis** (optional cache, content-addressed keys, circuit breaker) + **FastAPI** advisory
AI (semantic overlap suggestions with certainty bands; grounded explanations).

**Differentiator.** We separate deterministic financial truth from AI.
- Money is exact (integer paise, rational arithmetic): 200000 / 60000 / 180000 → 20 months, computed, not hard-coded.
- Null is unknown, never zero; missing recurring cost gives "payback unknown", not a flattering number.
- One benefit can only be counted once: AI4 + AI5 are recognised at ₹1,80,000, not ₹3,60,000, through an auditable ledger.
- Quality failure is a STOP even when payback looks great.
- **AI can explain. AI cannot change the money.** Kill the AI service and every number, decision and approval is identical.

**Proof.** 113 Go tests (real PostgreSQL), 140 Python tests, a 40-check live API smoke test run with all services up,
FastAPI down and Redis down, 15 judge checks, AI evals with 130 hallucination attacks — all passing.
Honest limits: synthetic fixture, no real interviews yet, Docker/CI written but not executed on our machine.
