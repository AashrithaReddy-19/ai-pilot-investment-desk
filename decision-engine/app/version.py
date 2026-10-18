"""Engine versions, reported by GET /health and echoed in /defaults.

- RULES_VERSION: bump whenever any engine output changes for the same input. The gateway puts
  it in its cache key, so nobody edits a shared RULES_VERSION by hand.
- CONTRACT_VERSION: bump only on a breaking (non-additive) change to a published response;
  must match contracts/VERSION (checked by tests/test_contracts.py).
- FORMULA_VERSION: the cash-flow conventions in cashflow.py; stored next to every breakdown
  so a persisted result can be reproduced with the formulas that made it.
"""

RULES_VERSION = "10"
CONTRACT_VERSION = 1
FORMULA_VERSION = "cf-1"
