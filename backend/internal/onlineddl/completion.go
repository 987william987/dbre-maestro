package onlineddl

import (
	"fmt"
	"strings"
)

type ToolCompletion int

const (
	ToolCompletionUnknown ToolCompletion = iota
	ToolCompletionSucceeded
	ToolCompletionFailed
)

// InspectToolCompletion validates the terminal marker emitted by the pinned tool version.
func InspectToolCompletion(mode string, statement Statement, result ToolResult, processErr error) ToolCompletion {
	if processErr != nil {
		return ToolCompletionFailed
	}
	lines := strings.Split(strings.ReplaceAll(result.Stdout+"\n"+result.Stderr, "\r", "\n"), "\n")
	switch mode {
	case ModeGhost:
		if hasExactLine(lines, "# Done") {
			return ToolCompletionSucceeded
		}
	case ModePTOSC:
		table := fmt.Sprintf("`%s`.`%s`", quoteToolIdentifier(statement.Database), quoteToolIdentifier(statement.Table))
		if hasExactLine(lines, "Successfully altered "+table+".") {
			return ToolCompletionSucceeded
		}
		if hasExactLine(lines, table+" was not altered.") || hasExactLine(lines, "Altered "+table+" but there were errors or warnings.") {
			return ToolCompletionFailed
		}
	}
	return ToolCompletionUnknown
}

func quoteToolIdentifier(value string) string {
	return strings.ReplaceAll(value, "`", "``")
}

func hasExactLine(lines []string, expected string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == expected {
			return true
		}
	}
	return false
}
