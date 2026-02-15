package ism

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/index"
)

// AliasUpdater is the interface for updating aliases (avoids circular dependency).
// Implemented by index.AliasManager.
type AliasUpdater interface {
	Put(name string, indices []string) error
	GetIndices(name string) ([]string, error)
}

// Runner is the background ISM execution loop.
type Runner struct {
	manager      *Manager
	registry     *index.Registry
	aliasUpdater AliasUpdater
	interval     time.Duration
	cancel       context.CancelFunc
	done         chan struct{}
}

// NewRunner creates a new ISM runner.
func NewRunner(mgr *Manager, reg *index.Registry, interval time.Duration) *Runner {
	return &Runner{
		manager:  mgr,
		registry: reg,
		interval: interval,
	}
}

// SetAliasUpdater sets the alias updater for rollover actions.
func (r *Runner) SetAliasUpdater(au AliasUpdater) {
	r.aliasUpdater = au
}

// Start begins the ISM evaluation loop.
func (r *Runner) Start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.done = make(chan struct{})
	go r.run(runCtx)
	slog.Info("ISM: runner started", "interval", r.interval)
}

// Stop stops the runner and waits for completion.
func (r *Runner) Stop() {
	if r.cancel != nil {
		r.cancel()
		<-r.done
		slog.Info("ISM: runner stopped")
	}
}

func (r *Runner) run(ctx context.Context) {
	defer close(r.done)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.EvaluateAll(ctx)
		}
	}
}

// EvaluateAll processes all managed indices. Exported for testing.
func (r *Runner) EvaluateAll(ctx context.Context) {
	r.manager.mu.Lock()
	defer r.manager.mu.Unlock()

	// Collect index names to process (snapshot keys to avoid map mutation during iteration)
	indices := make([]string, 0, len(r.manager.states))
	for name := range r.manager.states {
		indices = append(indices, name)
	}
	sort.Strings(indices)

	for _, indexName := range indices {
		state, ok := r.manager.states[indexName]
		if !ok {
			continue // may have been removed by a delete action
		}

		if state.Failed {
			continue
		}

		policy, ok := r.manager.policies[state.PolicyName]
		if !ok {
			slog.Warn("ISM: policy not found for index", "index", indexName, "policy", state.PolicyName)
			continue
		}

		policyState := policy.FindState(state.CurrentState)
		if policyState == nil {
			slog.Error("ISM: state not found in policy", "index", indexName, "state", state.CurrentState)
			state.Failed = true
			state.LastError = fmt.Sprintf("state %q not found in policy %q", state.CurrentState, state.PolicyName)
			continue
		}

		// Phase 1: Execute pending actions
		if state.ActionIndex < len(policyState.Actions) {
			r.executeAction(ctx, indexName, state, policyState)
			continue
		}

		// Phase 2: Evaluate transitions
		r.evaluateTransitions(ctx, indexName, state, policyState)
	}

	// Batch save state
	if err := r.manager.saveStateLocked(); err != nil {
		slog.Error("ISM: failed to save state after evaluation", "error", err)
	}
}

func (r *Runner) executeAction(ctx context.Context, indexName string, state *IndexISMState, ps *PolicyState) {
	action := ps.Actions[state.ActionIndex]

	// Check backoff window
	if state.RetryCount > 0 {
		backoff := r.actionBackoff(action)
		if backoff > 0 {
			waitTime := backoff * time.Duration(1<<uint(state.RetryCount-1))
			if time.Since(state.LastExecuted) < waitTime {
				return // still in backoff window
			}
		}
	}

	var err error
	switch action.Type {
	case ActionClose:
		err = r.registry.CloseIndex(indexName)
	case ActionOpen:
		err = r.registry.OpenIndex(indexName)
	case ActionDelete:
		err = r.registry.Delete(indexName)
		if err == nil {
			delete(r.manager.states, indexName)
			slog.Info("ISM: index deleted", "index", indexName)
			return
		}
	case ActionReadOnly:
		err = r.registry.SetReadOnly(indexName, true)
	case ActionForceMerge:
		err = r.forceMerge(ctx, indexName)
	case ActionNotification:
		err = r.sendNotification(ctx, indexName, state, action)
	case ActionSnapshot:
		err = r.snapshot(ctx, indexName)
	case ActionRollover:
		err = r.rollover(ctx, indexName, action)
	default:
		err = fmt.Errorf("unsupported action type: %s", action.Type)
	}

	state.LastExecuted = time.Now().UTC()
	if err != nil {
		state.LastError = err.Error()
		state.RetryCount++
		maxRetries := 3 // default
		if action.Retry != nil {
			maxRetries = action.Retry.Count
		}
		if state.RetryCount > maxRetries {
			state.Failed = true
			slog.Error("ISM: action failed, retries exhausted",
				"index", indexName, "action", action.Type,
				"error", err, "retries", state.RetryCount)
		} else {
			slog.Warn("ISM: action failed, will retry",
				"index", indexName, "action", action.Type,
				"error", err, "retry", state.RetryCount)
		}
	} else {
		state.ActionIndex++
		state.RetryCount = 0
		state.LastError = ""
		slog.Info("ISM: action completed",
			"index", indexName, "action", action.Type,
			"action_index", state.ActionIndex)
	}
}

func (r *Runner) actionBackoff(a Action) time.Duration {
	if a.Retry == nil {
		return time.Minute // default 1 minute backoff
	}
	if a.Retry.Backoff.Duration > 0 {
		return a.Retry.Backoff.Duration
	}
	if a.Retry.Delay.Duration > 0 {
		return a.Retry.Delay.Duration
	}
	return time.Minute
}

func (r *Runner) evaluateTransitions(ctx context.Context, indexName string, state *IndexISMState, ps *PolicyState) {
	for _, t := range ps.Transitions {
		if t.Conditions == nil {
			// Unconditional transition
			r.transition(indexName, state, t.StateName)
			return
		}

		matched, err := r.evaluateCondition(ctx, indexName, state, t.Conditions)
		if err != nil {
			slog.Warn("ISM: condition evaluation error",
				"index", indexName, "target", t.StateName, "error", err)
			continue
		}
		if matched {
			r.transition(indexName, state, t.StateName)
			return
		}
	}
}

func (r *Runner) transition(indexName string, state *IndexISMState, targetState string) {
	slog.Info("ISM: transitioning",
		"index", indexName,
		"from", state.CurrentState,
		"to", targetState)
	state.CurrentState = targetState
	state.ActionIndex = 0
	state.RetryCount = 0
	state.LastError = ""
	state.StateEnteredAt = time.Now().UTC()
}

func (r *Runner) evaluateCondition(ctx context.Context, indexName string, state *IndexISMState, cond *TransitionConditions) (bool, error) {
	// All specified conditions must be true (AND logic)
	if cond.MinIndexAge != nil {
		idx, err := r.registry.GetAny(indexName)
		if err != nil {
			return false, err
		}
		age := time.Since(idx.Meta.CreatedAt)
		if age < cond.MinIndexAge.Duration {
			return false, nil
		}
	}

	if cond.MinDocCount != nil {
		count, err := r.getDocCount(ctx, indexName)
		if err != nil {
			return false, fmt.Errorf("doc count: %w", err)
		}
		if count < *cond.MinDocCount {
			return false, nil
		}
	}

	if cond.CronSchedule != "" {
		lastCheck := state.StateEnteredAt
		if !state.LastExecuted.IsZero() {
			lastCheck = state.LastExecuted
		}
		matched, err := CronMatches(cond.CronSchedule, lastCheck, time.Now())
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}

	return true, nil
}

func (r *Runner) getDocCount(ctx context.Context, indexName string) (int64, error) {
	idx, err := r.registry.Get(indexName)
	if err != nil {
		return 0, err
	}

	result, err := idx.Route(ctx, "SELECT COUNT(*) AS cnt FROM _docs", "")
	if err != nil {
		return 0, err
	}

	var total int64
	for _, row := range result.Rows {
		if v, ok := row["cnt"]; ok {
			switch n := v.(type) {
			case int64:
				total += n
			case float64:
				total += int64(n)
			}
		}
	}
	return total, nil
}

// forceMerge runs CHECKPOINT and VACUUM on all shards of an index.
func (r *Runner) forceMerge(ctx context.Context, indexName string) error {
	idx, err := r.registry.Get(indexName)
	if err != nil {
		return err
	}

	if err := idx.Manager.ExecuteOnAll(ctx, "CHECKPOINT"); err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	if err := idx.Manager.ExecuteOnAll(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	return nil
}

// notificationData is the template context for notification messages.
type notificationData struct {
	IndexName  string
	PolicyName string
	State      string
	Action     string
	Error      string
}

func (r *Runner) sendNotification(_ context.Context, indexName string, state *IndexISMState, action Action) error {
	urlVal, ok := action.Config["url"]
	if !ok {
		return fmt.Errorf("notification action missing 'url'")
	}
	url, ok := urlVal.(string)
	if !ok {
		return fmt.Errorf("notification 'url' must be a string")
	}

	// Build message from template
	msgTemplate := "Index {{.IndexName}} in state {{.State}} (policy: {{.PolicyName}})"
	if v, ok := action.Config["message"]; ok {
		if s, ok := v.(string); ok {
			msgTemplate = s
		}
	}

	tmpl, err := template.New("notification").Parse(msgTemplate)
	if err != nil {
		return fmt.Errorf("parse notification template: %w", err)
	}

	data := notificationData{
		IndexName:  indexName,
		PolicyName: state.PolicyName,
		State:      state.CurrentState,
		Action:     string(action.Type),
		Error:      state.LastError,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute notification template: %w", err)
	}

	body := fmt.Sprintf(`{"text":%q}`, buf.String())
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("notification webhook returned %d", resp.StatusCode)
	}
	return nil
}

// snapshot copies all shard files for an index to a snapshot directory.
func (r *Runner) snapshot(_ context.Context, indexName string) error {
	idx, err := r.registry.GetAny(indexName)
	if err != nil {
		return err
	}

	snapshotDir := filepath.Join(r.registry.BaseDir(), "snapshots", indexName,
		time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}

	dataDir := idx.DataDir()
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("read index data dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".duckdb") {
			continue
		}
		src := filepath.Join(dataDir, entry.Name())
		dst := filepath.Join(snapshotDir, entry.Name())
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("snapshot shard %s: %w", entry.Name(), err)
		}
	}

	slog.Info("ISM: snapshot created", "index", indexName, "dir", snapshotDir)
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// rolloverSuffixRe matches index names like "logs-000001".
var rolloverSuffixRe = regexp.MustCompile(`^(.+)-(\d{6})$`)

// rollover creates a new index with an incremented numeric suffix and updates the alias.
func (r *Runner) rollover(_ context.Context, indexName string, action Action) error {
	aliasVal, ok := action.Config["alias"]
	if !ok {
		return fmt.Errorf("rollover action missing 'alias'")
	}
	aliasName, ok := aliasVal.(string)
	if !ok {
		return fmt.Errorf("rollover 'alias' must be a string")
	}

	// Parse numeric suffix
	matches := rolloverSuffixRe.FindStringSubmatch(indexName)
	if matches == nil {
		return fmt.Errorf("rollover: index name %q must end with -NNNNNN (e.g., logs-000001)", indexName)
	}

	prefix := matches[1]
	num, _ := strconv.Atoi(matches[2])
	newName := fmt.Sprintf("%s-%06d", prefix, num+1)

	// Get existing index settings to replicate
	idx, err := r.registry.GetAny(indexName)
	if err != nil {
		return fmt.Errorf("rollover: get existing index: %w", err)
	}

	// Create new index
	_, err = r.registry.Create(newName, idx.Meta.Settings)
	if err != nil {
		return fmt.Errorf("rollover: create new index %q: %w", newName, err)
	}

	// Update alias to point to new index
	if r.aliasUpdater != nil {
		// Get existing alias indices, replace old with new
		existing, err := r.aliasUpdater.GetIndices(aliasName)
		if err != nil {
			// Alias doesn't exist yet — create pointing to new index
			existing = nil
		}
		newIndices := make([]string, 0, len(existing)+1)
		for _, idx := range existing {
			if idx != indexName {
				newIndices = append(newIndices, idx)
			}
		}
		newIndices = append(newIndices, newName)

		if err := r.aliasUpdater.Put(aliasName, newIndices); err != nil {
			return fmt.Errorf("rollover: update alias %q: %w", aliasName, err)
		}
	}

	slog.Info("ISM: rollover completed",
		"old_index", indexName,
		"new_index", newName,
		"alias", aliasName)

	return nil
}
