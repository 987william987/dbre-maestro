package job

import (
	"testing"
	"time"
)

func TestCronScheduleMatchesFixedMinute(t *testing.T) {
	schedule, err := parseCronSchedule("0 9 * * *")
	if err != nil {
		t.Fatalf("parse cron: %v", err)
	}

	if !schedule.matches(time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("expected daily 09:00 schedule to match exactly at 09:00")
	}
	if schedule.matches(time.Date(2026, 6, 20, 9, 1, 0, 0, time.UTC)) {
		t.Fatal("expected daily 09:00 schedule not to match at 09:01")
	}
}

func TestCronScheduleMatchesStep(t *testing.T) {
	schedule, err := parseCronSchedule("*/15 * * * *")
	if err != nil {
		t.Fatalf("parse cron: %v", err)
	}

	if !schedule.matches(time.Date(2026, 6, 20, 9, 30, 0, 0, time.UTC)) {
		t.Fatal("expected */15 schedule to match at minute 30")
	}
	if schedule.matches(time.Date(2026, 6, 20, 9, 31, 0, 0, time.UTC)) {
		t.Fatal("expected */15 schedule not to match at minute 31")
	}
}

func TestValidateCronExpressionRejectsInvalidExpressions(t *testing.T) {
	invalid := []string{
		"",
		"0 9 * *",
		"60 9 * * *",
		"0 24 * * *",
		"*/0 * * * *",
	}

	for _, expression := range invalid {
		if err := ValidateCronExpression(expression); err == nil {
			t.Fatalf("expected %q to be invalid", expression)
		}
	}
}
