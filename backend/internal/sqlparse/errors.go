package sqlparse

import "fmt"

type SyntaxError struct {
	StatementSeq int
	Message      string
}

func (e *SyntaxError) Error() string {
	if e == nil {
		return ""
	}
	if e.StatementSeq > 0 {
		return fmt.Sprintf("statement %d: %s", e.StatementSeq, e.Message)
	}
	return e.Message
}
