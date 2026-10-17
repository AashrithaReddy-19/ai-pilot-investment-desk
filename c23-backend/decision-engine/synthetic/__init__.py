"""Validation and comparison pipeline: official C23 fixture vs the SYNTHETIC comparison dataset.

Synthetic rows are dummy data for testing, comparison and scenario analysis. Nothing here is real-world evidence.
The production decision engine is the Go service (c23-rules-v1); `synthetic.rules` is an independent Python
re-implementation that is checked against the Go engine's own outputs (reports/synthetic/go_engine_outputs.json).

Run (from decision-engine/):  python -m synthetic.run
"""
