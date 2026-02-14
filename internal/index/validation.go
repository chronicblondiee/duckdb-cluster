package index

import (
	"fmt"
	"regexp"
)

var validNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,127}$`)

// ValidateName checks that an index name is valid.
// Names must be lowercase alphanumeric plus hyphens/underscores,
// start with a letter, and be at most 128 characters.
// Names starting with "_" are reserved for internal use.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("index name cannot be empty")
	}
	if name[0] == '_' {
		return fmt.Errorf("index names starting with '_' are reserved")
	}
	if !validNameRe.MatchString(name) {
		return fmt.Errorf("invalid index name %q: must be lowercase alphanumeric, hyphens, or underscores, start with a letter, max 128 chars", name)
	}
	return nil
}

// validateNameInternal allows reserved names (e.g., _default).
func validateNameInternal(name string) error {
	if name == "" {
		return fmt.Errorf("index name cannot be empty")
	}
	// Allow _default and similar internal names
	if name[0] == '_' {
		if len(name) > 128 {
			return fmt.Errorf("index name too long: %d chars (max 128)", len(name))
		}
		return nil
	}
	return ValidateName(name)
}

// ValidateShardCount checks that a shard count is within bounds.
func ValidateShardCount(n int) error {
	if n < 1 {
		return fmt.Errorf("shard count must be at least 1, got %d", n)
	}
	if n > 1000 {
		return fmt.Errorf("shard count too large: %d (max 1000)", n)
	}
	return nil
}
