package models

import "time"

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Rank orders severities so escalation can be compared.
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	}
	return 0
}

type Alert struct {
	ID         string     `json:"id"`
	Rule       string     `json:"rule"`
	Component  string     `json:"component"`
	Target     string     `json:"target,omitempty"`
	Severity   Severity   `json:"severity"`
	Title      string     `json:"title"`
	Reason     string     `json:"reason"`
	Value      float64    `json:"value"`
	Threshold  float64    `json:"threshold"`
	StartedAt  time.Time  `json:"startedAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
}
