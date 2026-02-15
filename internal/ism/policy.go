package ism

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration with human-readable YAML/JSON support.
// Accepts Go duration strings ("168h", "30m") and day shorthand ("7d", "30d").
type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		// try as number (nanoseconds)
		var ns int64
		if err2 := json.Unmarshal(b, &ns); err2 != nil {
			return fmt.Errorf("duration must be a string or number: %w", err)
		}
		d.Duration = time.Duration(ns)
		return nil
	}
	return d.parse(s)
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return d.String(), nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	return d.parse(value.Value)
}

func (d *Duration) parse(s string) error {
	if s == "" || s == "0" {
		d.Duration = 0
		return nil
	}
	// Support day shorthand: "7d" -> "168h"
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		d.Duration = time.Duration(days) * 24 * time.Hour
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

// ActionType enumerates supported ISM actions.
type ActionType string

const (
	ActionClose        ActionType = "close"
	ActionOpen         ActionType = "open"
	ActionDelete       ActionType = "delete"
	ActionReadOnly     ActionType = "read_only"
	ActionForceMerge   ActionType = "force_merge"
	ActionNotification ActionType = "notification"
	ActionSnapshot     ActionType = "snapshot"
	ActionRollover     ActionType = "rollover"
)

// Policy defines an ISM policy as a named state machine.
type Policy struct {
	Name         string        `json:"name" yaml:"name"`
	Description  string        `json:"description,omitempty" yaml:"description,omitempty"`
	DefaultState string        `json:"default_state" yaml:"default_state"`
	States       []PolicyState `json:"states" yaml:"states"`
	ISMTemplate  []ISMTemplate `json:"ism_template,omitempty" yaml:"ism_template,omitempty"`
	Version      int           `json:"version" yaml:"-"`
	LastUpdated  time.Time     `json:"last_updated" yaml:"-"`
}

// ISMTemplate defines glob patterns for auto-attaching policies to new indices.
type ISMTemplate struct {
	Pattern  string `json:"pattern" yaml:"pattern"`
	Priority int    `json:"priority" yaml:"priority"`
}

// PolicyState defines a single state within the policy state machine.
type PolicyState struct {
	Name        string       `json:"name" yaml:"name"`
	Actions     []Action     `json:"actions,omitempty" yaml:"actions,omitempty"`
	Transitions []Transition `json:"transitions,omitempty" yaml:"transitions,omitempty"`
}

// Action defines an operation to execute when an index enters a state.
type Action struct {
	Type    ActionType     `json:"type" yaml:"type"`
	Config  map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	Retry   *RetryConfig   `json:"retry,omitempty" yaml:"retry,omitempty"`
	Timeout Duration       `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// RetryConfig defines retry behavior for a failed action.
type RetryConfig struct {
	Count   int      `json:"count" yaml:"count"`
	Backoff Duration `json:"backoff,omitempty" yaml:"backoff,omitempty"`
	Delay   Duration `json:"delay,omitempty" yaml:"delay,omitempty"`
}

// Transition defines a condition to move from one state to another.
type Transition struct {
	StateName  string                `json:"state_name" yaml:"state_name"`
	Conditions *TransitionConditions `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// TransitionConditions are evaluated to determine if a transition should fire.
type TransitionConditions struct {
	MinIndexAge  *Duration `json:"min_index_age,omitempty" yaml:"min_index_age,omitempty"`
	MinDocCount  *int64    `json:"min_doc_count,omitempty" yaml:"min_doc_count,omitempty"`
	CronSchedule string    `json:"cron,omitempty" yaml:"cron,omitempty"`
}

// FindState returns the PolicyState with the given name, or nil.
func (p *Policy) FindState(name string) *PolicyState {
	for i := range p.States {
		if p.States[i].Name == name {
			return &p.States[i]
		}
	}
	return nil
}
