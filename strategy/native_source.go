package strategy

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SourceSpan points to a token using one-based UTF-8 byte columns, with an
// exclusive end. Source is authoring data, never passed to nfqws2.
type SourceSpan struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	End    int `json:"end"`
}

type sourceToken struct {
	SourceSpan
	text       string
	start, end int
}

type nativeOptionError struct {
	token sourceToken
	err   error
}

func (e *nativeOptionError) Error() string {
	key, _, _ := strings.Cut(e.token.text, "=")
	return fmt.Sprintf("line %d, column %d (%s): %v", e.token.Line, e.token.Column, key, e.err)
}

func (e *nativeOptionError) Unwrap() error { return e.err }

func (t sourceToken) invalid(err error) error { return &nativeOptionError{token: t, err: err} }

func nativeSource(text string) ([]sourceToken, error) {
	if !utf8.ValidString(text) || strings.ContainsFunc(text, func(c rune) bool {
		return unicode.IsControl(c) && c != '\r' && c != '\n' && c != '\t'
	}) {
		return nil, fmt.Errorf("%w: expected UTF-8 native options and comments, without control characters", ErrInvalidStrategy)
	}
	var tokens []sourceToken
	lineNumber, start := 1, 0
	for line := range strings.SplitSeq(text, "\n") {
		for i := 0; i < len(line); {
			if i == 0 && start == 0 && strings.HasPrefix(line, "\ufeff") {
				i = len("\ufeff")
				continue
			}
			if sourceSpace(line[i]) {
				i++
				continue
			}
			if line[i] == '#' {
				break
			}
			j, quoted := i, false
			for ; j < len(line) && (quoted || !sourceSpace(line[j])); j++ {
				if line[j] == '"' {
					quoted = !quoted
				}
			}
			tokens = append(tokens, sourceToken{SourceSpan{lineNumber, i + 1, j + 1}, line[i:j], start + i, start + j})
			i = j
		}
		start += len(line) + 1
		lineNumber++
	}
	return tokens, nil
}

func sourceSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' }
