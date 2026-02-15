package ism

import "fmt"

// ValidatePolicy checks a policy for structural correctness.
func ValidatePolicy(p *Policy) error {
	if p.Name == "" {
		return fmt.Errorf("policy name is required")
	}
	if p.DefaultState == "" {
		return fmt.Errorf("default_state is required")
	}
	if len(p.States) == 0 {
		return fmt.Errorf("at least one state is required")
	}

	stateNames := make(map[string]bool, len(p.States))
	for _, s := range p.States {
		if s.Name == "" {
			return fmt.Errorf("state name cannot be empty")
		}
		if stateNames[s.Name] {
			return fmt.Errorf("duplicate state name: %s", s.Name)
		}
		stateNames[s.Name] = true

		for i, a := range s.Actions {
			if err := validateAction(a); err != nil {
				return fmt.Errorf("state %q action[%d]: %w", s.Name, i, err)
			}
		}
		for i, t := range s.Transitions {
			if t.StateName == "" {
				return fmt.Errorf("state %q transition[%d]: state_name is required", s.Name, i)
			}
		}
	}

	if !stateNames[p.DefaultState] {
		return fmt.Errorf("default_state %q not found in states", p.DefaultState)
	}

	// Validate transition targets reference existing states
	for _, s := range p.States {
		for i, t := range s.Transitions {
			if !stateNames[t.StateName] {
				return fmt.Errorf("state %q transition[%d]: target %q not found in states", s.Name, i, t.StateName)
			}
		}
	}

	// Validate ISM templates
	for i, tmpl := range p.ISMTemplate {
		if tmpl.Pattern == "" {
			return fmt.Errorf("ism_template[%d]: pattern is required", i)
		}
	}

	return nil
}

var validActionTypes = map[ActionType]bool{
	ActionClose:        true,
	ActionOpen:         true,
	ActionDelete:       true,
	ActionReadOnly:     true,
	ActionForceMerge:   true,
	ActionNotification: true,
	ActionSnapshot:     true,
	ActionRollover:     true,
}

func validateAction(a Action) error {
	if !validActionTypes[a.Type] {
		return fmt.Errorf("unknown action type: %s", a.Type)
	}
	if a.Type == ActionNotification {
		if _, ok := a.Config["url"]; !ok {
			return fmt.Errorf("notification action requires 'url' in config")
		}
	}
	if a.Type == ActionRollover {
		if _, ok := a.Config["alias"]; !ok {
			return fmt.Errorf("rollover action requires 'alias' in config")
		}
	}
	if a.Retry != nil && a.Retry.Count < 0 {
		return fmt.Errorf("retry count cannot be negative")
	}
	return nil
}
