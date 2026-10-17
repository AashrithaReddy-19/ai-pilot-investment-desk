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
}

type PortfolioIn struct {
	Name            string
	Mode            string
	BudgetINR       *float64
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
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ObservationIn struct {
	PilotID     string
	AdoptionPct *float64
	QualityPct  *float64
	Source      *string
}

type Observation struct {
	ID          int64     `json:"id"`
	PilotID     string    `json:"pilot_id"`
	AdoptionPct *float64  `json:"adoption_pct"`
	QualityPct  *float64  `json:"quality_pct"`
	ObservedOn  string    `json:"observed_on"`
	Source      *string   `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
}

type DecisionIn struct {
	PilotID   string
	Decision  string // GO | REVISE | STOP
	Metric    string
	Threshold *float64
	Actual    *float64
	Reason    string
}

type Decision struct {
	ID            int64     `json:"id"`
	PilotID       string    `json:"pilot_id"`
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

type EvaluationIn struct {
	Request      json.RawMessage
	Independent  json.RawMessage
	Conservative json.RawMessage
}

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
