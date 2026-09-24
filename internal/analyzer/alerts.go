package analyzer

import (
	"fmt"
	"sort"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/models"
)

// Level is one severity step of a rule, e.g. warning at 90%.
type Level struct {
	Severity  models.Severity
	Threshold float64
}

// Observation is a single measurement checked against a rule. Key must be
// unique per rule and target (e.g. "disk_free:C:").
type Observation struct {
	Key       string
	Rule      string
	Component string
	Target    string
	Title     string
	Unit      string
	Value     float64
	// Below flips the comparison: the rule fires when Value <= Threshold.
	Below   bool
	Levels  []Level // ascending severity
	Sustain time.Duration
	// Hysteresis is how far past the lowest threshold the value must recover
	// before the alert resolves, which prevents flapping around the line.
	Hysteresis float64
}

func (o Observation) breaches(threshold float64) bool {
	if o.Below {
		return o.Value <= threshold
	}
	return o.Value >= threshold
}

func (o Observation) recovered() bool {
	lowest := o.Levels[0].Threshold
	if o.Below {
		return o.Value > lowest+o.Hysteresis
	}
	return o.Value < lowest-o.Hysteresis
}

type EventType string

const (
	EventOpened    EventType = "opened"
	EventEscalated EventType = "escalated"
	EventResolved  EventType = "resolved"
)

type Event struct {
	Type  EventType
	Alert models.Alert
}

type ruleState struct {
	breachSince map[models.Severity]time.Time
	active      *models.Alert
	resolvedAt  time.Time
}

// AlertEngine is a deterministic state machine: a rule opens an alert only
// after its threshold has been breached continuously for the sustain period,
// escalates if a higher level is sustained, resolves once the value recovers
// past the hysteresis band, and then stays quiet for the cooldown.
type AlertEngine struct {
	states map[string]*ruleState
}

func NewAlertEngine() *AlertEngine {
	return &AlertEngine{states: map[string]*ruleState{}}
}

func (e *AlertEngine) Evaluate(now time.Time, obs []Observation, cooldown time.Duration) []Event {
	var events []Event
	seen := make(map[string]bool, len(obs))
	for _, o := range obs {
		if len(o.Levels) == 0 {
			continue
		}
		seen[o.Key] = true
		st := e.states[o.Key]
		if st == nil {
			st = &ruleState{breachSince: map[models.Severity]time.Time{}}
			e.states[o.Key] = st
		}
		if ev, ok := e.step(st, o, now, cooldown); ok {
			events = append(events, ev)
		}
	}
	// A target that stopped reporting (drive removed, sensor lost) can't be
	// evaluated any more, so its alert is closed rather than left dangling.
	for key, st := range e.states {
		if seen[key] {
			continue
		}
		if st.active != nil {
			events = append(events, resolve(st, now, "no longer reported"))
		}
		delete(e.states, key)
	}
	return events
}

func (e *AlertEngine) step(st *ruleState, o Observation, now time.Time, cooldown time.Duration) (Event, bool) {
	var sustained *Level
	for i := range o.Levels {
		lvl := o.Levels[i]
		if !o.breaches(lvl.Threshold) {
			delete(st.breachSince, lvl.Severity)
			continue
		}
		since, ok := st.breachSince[lvl.Severity]
		if !ok {
			since = now
			st.breachSince[lvl.Severity] = now
		}
		if now.Sub(since) >= o.Sustain {
			sustained = &lvl
		}
	}

	if st.active != nil {
		st.active.Value = o.Value
		if o.recovered() {
			return resolve(st, now, ""), true
		}
		if sustained != nil && sustained.Severity.Rank() > st.active.Severity.Rank() {
			st.active.Severity = sustained.Severity
			st.active.Threshold = sustained.Threshold
			st.active.Reason = reason(o, *sustained)
			st.active.UpdatedAt = now
			return Event{Type: EventEscalated, Alert: *st.active}, true
		}
		return Event{}, false
	}

	if sustained == nil || (!st.resolvedAt.IsZero() && now.Sub(st.resolvedAt) < cooldown) {
		return Event{}, false
	}
	st.active = &models.Alert{
		ID:        fmt.Sprintf("%s@%d", o.Key, now.UnixMilli()),
		Rule:      o.Rule,
		Component: o.Component,
		Target:    o.Target,
		Severity:  sustained.Severity,
		Title:     o.Title,
		Reason:    reason(o, *sustained),
		Value:     o.Value,
		Threshold: sustained.Threshold,
		StartedAt: now,
		UpdatedAt: now,
	}
	return Event{Type: EventOpened, Alert: *st.active}, true
}

func resolve(st *ruleState, now time.Time, note string) Event {
	a := *st.active
	a.ResolvedAt = &now
	a.UpdatedAt = now
	if note != "" {
		a.Reason += " (" + note + ")"
	}
	st.active = nil
	st.resolvedAt = now
	return Event{Type: EventResolved, Alert: a}
}

func reason(o Observation, lvl Level) string {
	cmp := "≥"
	if o.Below {
		cmp = "≤"
	}
	s := fmt.Sprintf("%s is %.1f%s (%s threshold %s %.0f%s)", o.Title, o.Value, o.Unit, lvl.Severity, cmp, lvl.Threshold, o.Unit)
	if o.Sustain > 0 {
		s += " for at least " + humanDuration(o.Sustain)
	}
	return s
}

func humanDuration(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		if d == time.Minute {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	return fmt.Sprintf("%d seconds", int(d.Seconds()))
}

// Active returns currently open alerts, most severe first.
func (e *AlertEngine) Active() []models.Alert {
	out := []models.Alert{}
	for _, st := range e.states {
		if st.active != nil {
			out = append(out, *st.active)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity.Rank() != out[j].Severity.Rank() {
			return out[i].Severity.Rank() > out[j].Severity.Rank()
		}
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out
}
