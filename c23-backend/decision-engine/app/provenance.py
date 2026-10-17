"""Provenance labels. Every derived row/field says where its value came from."""

OFFICIAL_FIXTURE = "OFFICIAL_FIXTURE"      # value read unchanged from the C23 fixture
TEAM_ASSUMPTION = "TEAM_ASSUMPTION"        # team-authored input or a documented imputation rule
SYNTHETIC_TRAINING = "SYNTHETIC_TRAINING"  # generated rows for optional ML; never mixed with fixture rows
OBSERVED = "OBSERVED"                      # measured during a pilot (adoption, quality)
CALCULATED = "CALCULATED"                  # pure function of other fields
UNKNOWN = "UNKNOWN"                        # null / absent: unknown, never zero

ALL = (OFFICIAL_FIXTURE, TEAM_ASSUMPTION, SYNTHETIC_TRAINING, OBSERVED, CALCULATED, UNKNOWN)
