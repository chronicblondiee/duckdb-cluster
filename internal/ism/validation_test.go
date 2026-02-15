package ism

import (
	"testing"
)

func TestValidatePolicy_Valid(t *testing.T) {
	p := &Policy{
		Name:         "test-policy",
		DefaultState: "hot",
		States: []PolicyState{
			{
				Name: "hot",
				Transitions: []Transition{
					{StateName: "delete"},
				},
			},
			{
				Name: "delete",
				Actions: []Action{
					{Type: ActionDelete},
				},
			},
		},
	}
	if err := ValidatePolicy(p); err != nil {
		t.Fatalf("expected valid policy, got error: %v", err)
	}
}

func TestValidatePolicy_EmptyName(t *testing.T) {
	p := &Policy{DefaultState: "hot", States: []PolicyState{{Name: "hot"}}}
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidatePolicy_EmptyDefaultState(t *testing.T) {
	p := &Policy{Name: "p", States: []PolicyState{{Name: "hot"}}}
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("expected error for empty default_state")
	}
}

func TestValidatePolicy_NoStates(t *testing.T) {
	p := &Policy{Name: "p", DefaultState: "hot"}
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("expected error for no states")
	}
}

func TestValidatePolicy_DuplicateState(t *testing.T) {
	p := &Policy{
		Name:         "p",
		DefaultState: "hot",
		States:       []PolicyState{{Name: "hot"}, {Name: "hot"}},
	}
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("expected error for duplicate state name")
	}
}

func TestValidatePolicy_DefaultStateNotFound(t *testing.T) {
	p := &Policy{
		Name:         "p",
		DefaultState: "missing",
		States:       []PolicyState{{Name: "hot"}},
	}
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("expected error for missing default_state")
	}
}

func TestValidatePolicy_TransitionTargetNotFound(t *testing.T) {
	p := &Policy{
		Name:         "p",
		DefaultState: "hot",
		States: []PolicyState{
			{Name: "hot", Transitions: []Transition{{StateName: "missing"}}},
		},
	}
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("expected error for transition target not found")
	}
}

func TestValidatePolicy_UnknownActionType(t *testing.T) {
	p := &Policy{
		Name:         "p",
		DefaultState: "hot",
		States: []PolicyState{
			{Name: "hot", Actions: []Action{{Type: "bogus"}}},
		},
	}
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("expected error for unknown action type")
	}
}

func TestValidatePolicy_NotificationRequiresURL(t *testing.T) {
	p := &Policy{
		Name:         "p",
		DefaultState: "hot",
		States: []PolicyState{
			{Name: "hot", Actions: []Action{{Type: ActionNotification}}},
		},
	}
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("expected error for notification without url")
	}
}

func TestValidatePolicy_RolloverRequiresAlias(t *testing.T) {
	p := &Policy{
		Name:         "p",
		DefaultState: "hot",
		States: []PolicyState{
			{Name: "hot", Actions: []Action{{Type: ActionRollover}}},
		},
	}
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("expected error for rollover without alias")
	}
}

func TestValidatePolicy_EmptyISMTemplatePattern(t *testing.T) {
	p := &Policy{
		Name:         "p",
		DefaultState: "hot",
		States:       []PolicyState{{Name: "hot"}},
		ISMTemplate:  []ISMTemplate{{Pattern: ""}},
	}
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("expected error for empty ISM template pattern")
	}
}

func TestValidatePolicy_AllActionTypes(t *testing.T) {
	actions := []Action{
		{Type: ActionClose},
		{Type: ActionOpen},
		{Type: ActionDelete},
		{Type: ActionReadOnly},
		{Type: ActionForceMerge},
		{Type: ActionNotification, Config: map[string]any{"url": "http://example.com"}},
		{Type: ActionSnapshot},
		{Type: ActionRollover, Config: map[string]any{"alias": "my-alias"}},
	}

	p := &Policy{
		Name:         "all-actions",
		DefaultState: "s",
		States:       []PolicyState{{Name: "s", Actions: actions}},
	}
	if err := ValidatePolicy(p); err != nil {
		t.Fatalf("expected valid policy with all action types, got: %v", err)
	}
}

func TestValidatePolicy_EmptyStateName(t *testing.T) {
	p := &Policy{
		Name:         "p",
		DefaultState: "",
		States:       []PolicyState{{Name: ""}},
	}
	// Should fail on empty default_state before reaching state validation
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("expected error")
	}
}
