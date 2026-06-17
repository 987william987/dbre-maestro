package timeutil

import "time"

// NowUTC returns the current wall-clock time normalized to UTC for persistence.
func NowUTC() time.Time {
	return time.Now().UTC()
}
