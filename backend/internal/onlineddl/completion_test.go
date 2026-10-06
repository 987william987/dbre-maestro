package onlineddl

import (
	"errors"
	"testing"
)

func TestInspectToolCompletionRequiresExitCodeAndModeSpecificTerminalMarker(t *testing.T) {
	statement := Statement{Database: "app", Table: "orders"}
	tests := []struct {
		name       string
		mode       string
		result     ToolResult
		processErr error
		want       ToolCompletion
	}{
		{"ptosc success", ModePTOSC, ToolResult{Stdout: "Copied rows OK.\nSuccessfully altered `app`.`orders`.\n"}, nil, ToolCompletionSucceeded},
		{"ptosc explicit failure despite zero exit", ModePTOSC, ToolResult{Stdout: "`app`.`orders` was not altered.\n"}, nil, ToolCompletionFailed},
		{"ptosc warning terminal", ModePTOSC, ToolResult{Stdout: "Altered `app`.`orders` but there were errors or warnings.\n"}, nil, ToolCompletionFailed},
		{"ptosc missing terminal", ModePTOSC, ToolResult{Stdout: "Swapped original and new tables OK.\n"}, nil, ToolCompletionUnknown},
		{"ghost success", ModeGhost, ToolResult{Stdout: "# Done\n"}, nil, ToolCompletionSucceeded},
		{"ghost missing terminal", ModeGhost, ToolResult{Stdout: "Copy: 100.0%\n"}, nil, ToolCompletionUnknown},
		{"nonzero always fails", ModePTOSC, ToolResult{Stdout: "Successfully altered `app`.`orders`.\n"}, errors.New("exit 1"), ToolCompletionFailed},
		{"quoted identifier", ModePTOSC, ToolResult{Stdout: "Successfully altered `app`.`odd``name`.\n"}, nil, ToolCompletionUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InspectToolCompletion(tt.mode, statement, tt.result, tt.processErr); got != tt.want {
				t.Fatalf("completion=%v want=%v", got, tt.want)
			}
		})
	}
	quoted := Statement{Database: "app", Table: "odd`name"}
	if got := InspectToolCompletion(ModePTOSC, quoted, ToolResult{Stdout: "Successfully altered `app`.`odd``name`.\n"}, nil); got != ToolCompletionSucceeded {
		t.Fatalf("quoted completion=%v", got)
	}
}
