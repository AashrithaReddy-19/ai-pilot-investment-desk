package proposalextract

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

var FieldNames = []string{
	"id", "task", "setup_cost_inr", "annual_benefit_inr", "annual_running_cost_inr",
	"benefit_type", "overlap_group", "adoption_pct", "readiness", "ramp_months", "delay_months",
}

var benefitTypes = map[string]bool{
	"cashable": true, "cashable assumption": true, "time freed": true, "non-cashable": true, "unknown": true,
}

var readinessValues = map[string]bool{"low": true, "medium": true, "high": true}
var proposalNamePattern = regexp.MustCompile(`^[A-Za-z0-9_\- ]{1,80}$`)

// ValidateFieldValue validates one corrected or model-produced value against the existing assessment contract.
// It deliberately does not calculate financial values or infer missing values.
func ValidateFieldValue(name string, value any) error {
	if value == nil {
		if name == "annual_running_cost_inr" || name == "benefit_type" || name == "overlap_group" ||
			name == "adoption_pct" || name == "readiness" || name == "ramp_months" || name == "delay_months" {
			return nil
		}
		return fmt.Errorf("%s is required", name)
	}
	switch name {
	case "id":
		s, ok := value.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s must be non-empty text", name)
		}
		if len([]rune(s)) > 200 {
			return fmt.Errorf("%s is too long", name)
		}
	case "task":
		s, ok := value.(string)
		if !ok || !proposalNamePattern.MatchString(strings.TrimSpace(s)) {
			return errors.New("task must be 1-80 characters using letters, digits, spaces, hyphens or underscores")
		}
	case "overlap_group":
		s, ok := value.(string)
		if !ok || len([]rune(s)) > 40 {
			return errors.New("overlap_group must be text of at most 40 characters")
		}
	case "benefit_type":
		s, ok := value.(string)
		if !ok || !benefitTypes[s] {
			return fmt.Errorf("benefit_type must be one of cashable, cashable assumption, time freed, non-cashable or unknown")
		}
	case "readiness":
		s, ok := value.(string)
		if !ok || !readinessValues[s] {
			return fmt.Errorf("readiness must be low, medium or high")
		}
	case "setup_cost_inr", "annual_benefit_inr", "annual_running_cost_inr":
		return validateNumber(name, value, 0, math.MaxFloat64, false)
	case "adoption_pct":
		return validateNumber(name, value, 0, 100, false)
	case "ramp_months", "delay_months":
		return validateNumber(name, value, 0, 120, true)
	default:
		return fmt.Errorf("unknown extraction field %q", name)
	}
	return nil
}

func validateNumber(name string, value any, min, max float64, whole bool) error {
	n, ok := value.(float64)
	if !ok {
		if integer, integerOK := value.(int); integerOK {
			n, ok = float64(integer), true
		}
	}
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < min || n > max || whole && n != math.Trunc(n) {
		return fmt.Errorf("%s must be a valid number in the accepted range", name)
	}
	return nil
}

func ValidateForConfirmation(fields map[string]Field) error {
	for _, name := range FieldNames {
		field, ok := fields[name]
		if !ok || field.Status != "explicit" {
			return fmt.Errorf("%s requires explicit review or correction", name)
		}
		if err := ValidateFieldValue(name, field.Value); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMockResponse validates a provider response before it can be treated as extraction data.
// It is used by tests and by any future Bedrock adapter; raw model JSON must not bypass this boundary.
func ValidateMockResponse(raw []byte) (Result, error) {
	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return Result{}, fmt.Errorf("invalid extraction response JSON: %w", err)
	}
	if len(result.Fields) != len(FieldNames) {
		return Result{}, fmt.Errorf("extraction response must contain exactly %d fields", len(FieldNames))
	}
	for _, name := range FieldNames {
		field, ok := result.Fields[name]
		if !ok {
			return Result{}, fmt.Errorf("extraction response is missing field %q", name)
		}
		if field.Status == "explicit" || field.Status == "derived" {
			if field.Status == "derived" && strings.HasSuffix(name, "_inr") {
				return Result{}, fmt.Errorf("%s cannot be derived by the model; use an explicitly stated amount or null", name)
			}
			if err := ValidateFieldValue(name, field.Value); err != nil {
				return Result{}, fmt.Errorf("%s: %w", name, err)
			}
			if field.Status == "derived" && strings.TrimSpace(field.Explanation) == "" {
				return Result{}, fmt.Errorf("%s derived values must include an explanation", name)
			}
			if len(field.Evidence) == 0 {
				return Result{}, fmt.Errorf("%s must include evidence", name)
			}
			for _, evidence := range field.Evidence {
				if strings.TrimSpace(evidence.Source) == "" || strings.TrimSpace(evidence.Excerpt) == "" {
					return Result{}, fmt.Errorf("%s evidence must include a source and excerpt", name)
				}
			}
		} else if field.Status != "missing" && field.Status != "ambiguous" {
			return Result{}, fmt.Errorf("%s has invalid status %q", name, field.Status)
		} else if field.Value != nil {
			return Result{}, fmt.Errorf("%s must use null for %s status", name, field.Status)
		}
	}
	return result, nil
}
