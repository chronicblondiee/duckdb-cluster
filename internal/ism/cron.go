package ism

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// CronMatches returns true if the cron expression matched at any point
// between lastCheck and now.
func CronMatches(expr string, lastCheck, now time.Time) (bool, error) {
	schedule, err := cron.ParseStandard(expr)
	if err != nil {
		return false, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	next := schedule.Next(lastCheck)
	return !next.After(now), nil
}
