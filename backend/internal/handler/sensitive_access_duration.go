package handler

import "fmt"

const (
	defaultSensitiveAccessDurationMinutes = 10
	minSensitiveAccessDurationMinutes     = 1
	maxSensitiveAccessDurationMinutes     = 3 * 24 * 60
)

func normalizeSensitiveAccessDurationMinutes(value int) (int, error) {
	if value == 0 {
		value = defaultSensitiveAccessDurationMinutes
	}
	if value < minSensitiveAccessDurationMinutes || value > maxSensitiveAccessDurationMinutes {
		return 0, fmt.Errorf(
			"approved_duration_minutes must be between %d and %d",
			minSensitiveAccessDurationMinutes,
			maxSensitiveAccessDurationMinutes,
		)
	}
	return value, nil
}
