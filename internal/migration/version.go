package migration

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is set at build time via -ldflags:
//
//	-ldflags "-X github.com/chronicblondiee/duckdb-cluster/internal/migration.Version=1.0.0"
var Version = "0.0.0-dev"

// SemVer represents a parsed semantic version.
type SemVer struct {
	Major int
	Minor int
	Patch int
}

// ParseSemVer parses a "Major.Minor.Patch" string.
// Strips leading "v" and trailing suffixes like "-dev".
func ParseSemVer(s string) (SemVer, error) {
	s = strings.TrimPrefix(s, "v")
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return SemVer{}, fmt.Errorf("invalid semver: %q", s)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return SemVer{}, fmt.Errorf("invalid major version: %w", err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return SemVer{}, fmt.Errorf("invalid minor version: %w", err)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return SemVer{}, fmt.Errorf("invalid patch version: %w", err)
	}
	return SemVer{Major: major, Minor: minor, Patch: patch}, nil
}

func (v SemVer) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Less returns true if v < other.
func (v SemVer) Less(other SemVer) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}
	return v.Patch < other.Patch
}

// Equal returns true if v == other.
func (v SemVer) Equal(other SemVer) bool {
	return v.Major == other.Major && v.Minor == other.Minor && v.Patch == other.Patch
}
