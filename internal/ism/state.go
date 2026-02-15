package ism

import "time"

// IndexISMState tracks the ISM execution state for a single index.
type IndexISMState struct {
	PolicyName     string    `json:"policy_name"`
	PolicyVersion  int       `json:"policy_version"`
	CurrentState   string    `json:"current_state"`
	StateEnteredAt time.Time `json:"state_entered_at"`
	ActionIndex    int       `json:"action_index"`
	RetryCount     int       `json:"retry_count"`
	LastError      string    `json:"last_error,omitempty"`
	LastExecuted   time.Time `json:"last_executed,omitempty"`
	Failed         bool      `json:"failed"`
}

// ISMStatus is the API-facing status for an index.
type ISMStatus struct {
	Index          string    `json:"index"`
	PolicyName     string    `json:"policy_name"`
	PolicyVersion  int       `json:"policy_version"`
	CurrentState   string    `json:"current_state"`
	StateEnteredAt time.Time `json:"state_entered_at"`
	ActionIndex    int       `json:"action_index"`
	RetryCount     int       `json:"retry_count"`
	LastError      string    `json:"last_error,omitempty"`
	Failed         bool      `json:"failed"`
}

// toStatus converts internal state to API-facing status.
func (s *IndexISMState) toStatus(indexName string) ISMStatus {
	return ISMStatus{
		Index:          indexName,
		PolicyName:     s.PolicyName,
		PolicyVersion:  s.PolicyVersion,
		CurrentState:   s.CurrentState,
		StateEnteredAt: s.StateEnteredAt,
		ActionIndex:    s.ActionIndex,
		RetryCount:     s.RetryCount,
		LastError:      s.LastError,
		Failed:         s.Failed,
	}
}
