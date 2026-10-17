// Package model holds plain data types shared between packages.
package model

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

// Upstream is a raw response from the FastAPI rules engine.
type Upstream struct {
	Status      int
	Body        []byte
	ContentType string
}

// PortfolioPilot is one pilot's outcome inside a saved portfolio. NULL means unknown, never zero.
type PortfolioPilot struct {
	PilotID       string   `json:"pilot_id"`
	Selected      bool     `json:"selected"`
	Reason        *string  `json:"reason"`
	BenefitGroup  *string  `json:"benefit_group"`
	SetupINR      *float64 `json:"setup_inr"`
	RunINR        *float64 `json:"run_inr"`
	RunAssumed    bool     `json:"run_assumed"`
	NetINR        *float64 `json:"net_inr"`
	PaybackMonths *float64 `json:"payback_months"` // nil = never pays back
	ProfitINR     *float64 `json:"profit_inr"`     // multi-period: profit over the horizon
	ROIPct        *float64 `json:"roi_pct"`        // multi-period: roi_pct; nil = undefined (no one-time cost)
}

type PortfolioIn struct {
	Name            string
	Mode            string
	BudgetINR       *float64
	HorizonMonths   *int
	DiscountRatePct *float64
	StartingState   json.RawMessage
	NegotiatedScope json.RawMessage
	FinalEvidence   json.RawMessage
	Pilots          []PortfolioPilot
}

type Portfolio struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Mode            string           `json:"mode"`
	BudgetINR       *float64         `json:"budget_inr"`
	HorizonMonths   *int             `json:"horizon_months"`
	DiscountRatePct *float64         `json:"discount_rate_pct"`
	StartingState   json.RawMessage  `json:"starting_state"`
	NegotiatedScope json.RawMessage  `json:"negotiated_scope"`
	FinalEvidence   json.RawMessage  `json:"final_evidence"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	Pilots          []PortfolioPilot `json:"pilots"`
}

type PortfolioSummary struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Mode          string    `json:"mode"`
	BudgetINR     *float64  `json:"budget_inr"`
	SelectedCount int       `json:"selected_count"`
	HorizonMonths *int      `json:"horizon_months"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ObservationIn struct {
	PilotID     string
	ProposalID  *string // optional link to a proposal
	AdoptionPct *float64
	QualityPct  *float64
	Source      *string
}

type Observation struct {
	ID          int64     `json:"id"`
	PilotID     string    `json:"pilot_id"`
	ProposalID  *string   `json:"proposal_id"`
	AdoptionPct *float64  `json:"adoption_pct"`
	QualityPct  *float64  `json:"quality_pct"`
	ObservedOn  string    `json:"observed_on"`
	Source      *string   `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
}

type DecisionIn struct {
	PilotID    string
	ProposalID *string // optional link to a proposal
	Decision   string  // GO | REVISE | STOP
	Metric     string
	Threshold  *float64
	Actual     *float64
	Reason     string
}

type Decision struct {
	ID            int64     `json:"id"`
	PilotID       string    `json:"pilot_id"`
	ProposalID    *string   `json:"proposal_id"`
	Decision      string    `json:"decision"`
	Metric        string    `json:"metric"`
	Threshold     *float64  `json:"threshold"`
	Actual        *float64  `json:"actual"`
	Reason        string    `json:"reason"`
	ObservationID *int64    `json:"observation_id"`
	CreatedAt     time.Time `json:"created_at"`
}

type History struct {
	Observations []Observation `json:"observations"`
	Decisions    []Decision    `json:"decisions"`
}

// HistoryEvent is one row of the merged evaluation + decision timeline. What changed is
// summarised per event; before/after is previous_verdict for decisions, diffed selected for evaluations.
type HistoryEvent struct {
	ID   int64     `json:"id"`
	Kind string    `json:"kind"`
	At   time.Time `json:"at"`
	// decision side
	PilotID          *string  `json:"pilot_id"`
	ProposalID       *string  `json:"proposal_id"`
	Verdict          *string  `json:"verdict"`
	PrevVerdict      *string  `json:"previous_verdict"`
	ObservedAdoption *float64 `json:"observed_adoption_pct"`
	ObservedQuality  *float64 `json:"observed_quality_pct"`
	Reason           *string  `json:"reason"`
	// evaluation side
	Mode          *string  `json:"mode"`
	Budget        *float64 `json:"budget"`
	Selected      []string `json:"selected"`
	ChangeSummary *string  `json:"change_summary"`
}

type EvaluationIn struct {
	Request      json.RawMessage
	Independent  json.RawMessage
	Conservative json.RawMessage
}

// EvaluationSummary is one row of the evaluations audit log (no bodies).
type EvaluationSummary struct {
	ID          int64     `json:"id"`
	PortfolioID *string   `json:"portfolio_id"` // set when the evaluation's portfolio was saved
	CreatedAt   time.Time `json:"created_at"`
}

// Evaluation is the full audit row: the request and both answers, as received.
type Evaluation struct {
	ID           int64           `json:"id"`
	PortfolioID  *string         `json:"portfolio_id"`
	Request      json.RawMessage `json:"request"`
	Independent  json.RawMessage `json:"independent"`
	Conservative json.RawMessage `json:"conservative"`
	CreatedAt    time.Time       `json:"created_at"`
}

// ---------- proposals ----------

// ProposalIn is the body for create / update.
type ProposalIn struct {
	Name   string
	Inputs json.RawMessage // fixture pilot row + extension overrides as one JSON doc
	Notes  *string
}

type Proposal struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Status    string          `json:"status"` // active | archived
	Inputs    json.RawMessage `json:"inputs"`
	Notes     *string         `json:"notes"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type ProposalSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Notes     *string   `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ---------- evidence ----------

type EvidenceIn struct {
	ProposalID string
	Kind       string  // interview | measurement | document | assumption
	Source     string  // who / what provided this
	Quality    *string // observed | assumed | calculated | fixture
	Note       *string
}

type Evidence struct {
	ID         int64     `json:"id"`
	ProposalID string    `json:"proposal_id"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source"`
	Quality    *string   `json:"quality"`
	Note       *string   `json:"note"`
	CreatedAt  time.Time `json:"created_at"`
}

// ---------- interviews ----------

type InterviewIn struct {
	IntervieweeRole string   `json:"interviewee_role"`
	Task            string   `json:"task"`
	MinutesPerTask  *float64 `json:"minutes_per_task"`
	UsefulResult    *string  `json:"useful_result"`
	Quote           *string  `json:"quote"`
	InterviewedOn   *string  `json:"interviewed_on"` // YYYY-MM-DD
}

type Interview struct {
	ID              int       `json:"id"`
	IntervieweeRole string    `json:"interviewee_role"`
	Task            string    `json:"task"`
	MinutesPerTask  *float64  `json:"minutes_per_task"`
	UsefulResult    *string   `json:"useful_result"`
	Quote           *string   `json:"quote"`
	InterviewedOn   *string   `json:"interviewed_on"`
	CreatedAt       time.Time `json:"created_at"`
}

// ---------- Phase 6: pilot plans (checkpoint tracker) ----------

// PilotPlan is one pilot's recorded 30-day checkpoint plan. Expected values come from the
// engine's plan generator; actuals are joined from the observations table by the tracker.
type PilotPlan struct {
	PilotID        string    `json:"pilot_id"`
	Objective      string    `json:"objective"`
	Owner          *string   `json:"owner"`
	AdoptionTarget *float64  `json:"adoption_target_pct"`
	QualityTarget  *float64  `json:"quality_target_pct"`
	CostLimitINR   *float64  `json:"cost_limit_inr"`
	DurationDays   int       `json:"duration_days"`
	StopRule       string    `json:"stop_rule"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PlanStatusIn struct{ Status string }
