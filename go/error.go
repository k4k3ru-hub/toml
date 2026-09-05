package toml

import "fmt"

// ParseError reports a TOML syntax error location.
type ParseError struct {
	Line    int
	Column  int
	Message string
}

// Error returns the formatted TOML syntax error.
//
// Returns:
//   - Syntax error with line and column.
//
// Version:
//   - 2026-08-28: Added.
func (e *ParseError) Error() string {
	if e == nil {
		return "toml parse error"
	}
	return fmt.Sprintf("toml parse error: %s: line=%d column=%d", e.Message, e.Line, e.Column)
}
