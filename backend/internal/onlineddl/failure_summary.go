package onlineddl

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxFailureSummaryBytes = 1024

var (
	failureLinePattern  = regexp.MustCompile(`(?i)\b(error|failed|failure|denied|refused|privilege|fatal)\b`)
	secretPattern       = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret)\s*[=:]\s*([^\s;]+)`)
	dsnPattern          = regexp.MustCompile(`(?i)DBI:mysql:[^\s]+`)
	defaultsPathPattern = regexp.MustCompile(`(?i)(--defaults-file(?:=|\s+))[^\s]+`)
	tempPathPattern     = regexp.MustCompile(`/[^\s]*/maestro-online-ddl-[^\s/]+(?:/[^\s]+)?`)
)

// FailureSummary retains only bounded diagnostic lines suitable for persistence.
func FailureSummary(result ToolResult) string {
	for _, output := range []string{result.Stderr, result.Stdout} {
		if summary := summarizeFailureOutput(output); summary != "" {
			return summary
		}
	}
	return ""
}

func summarizeFailureOutput(output string) string {
	lines := strings.Split(strings.ReplaceAll(output, "\r", "\n"), "\n")
	selected := make([]string, 0, 3)
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" || !failureLinePattern.MatchString(line) {
			continue
		}
		line = secretPattern.ReplaceAllString(line, "$1=[REDACTED]")
		line = dsnPattern.ReplaceAllString(line, "[REDACTED_DSN]")
		line = defaultsPathPattern.ReplaceAllString(line, "$1[REDACTED_PATH]")
		line = tempPathPattern.ReplaceAllString(line, "[REDACTED_PATH]")
		selected = append(selected, line)
		if len(selected) == 3 {
			break
		}
	}
	return truncateSummary(strings.Join(selected, " | "))
}

func truncateSummary(value string) string {
	value = strings.ToValidUTF8(value, "?")
	if len(value) <= maxFailureSummaryBytes {
		return value
	}
	end := maxFailureSummaryBytes - 3
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return strings.TrimSpace(value[:end]) + "..."
}
