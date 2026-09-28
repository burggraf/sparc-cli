package database

import (
	"bytes"
	"context"
	"sync"

	"github.com/burggraf/sparc-cli/internal/tools"
)

// ponytail: caps SQL lines at 16 MiB; use a chunked lexical scanner if real dumps exceed it.
const maxPublicSchemaSQLLineBytes = maxFingerprintCopyLineBytes

type publicSchemaTransformSink struct {
	downstream   tools.AbortableOutputSink
	line         []byte
	lex          pgSQLLexState
	replacements int
	closed       bool
	err          error
	mu           sync.Mutex
}

func newPublicSchemaTransformSink(downstream tools.AbortableOutputSink) *publicSchemaTransformSink {
	return &publicSchemaTransformSink{downstream: downstream}
}

func (s *publicSchemaTransformSink) WriteContext(ctx context.Context, data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || s.closed || s.downstream == nil {
		return 0, tools.ErrOutput
	}
	for _, value := range data {
		if value == '\n' {
			if err := s.writeLine(ctx, true); err != nil {
				return 0, err
			}
			s.line = s.line[:0]
			continue
		}
		if len(s.line) >= maxPublicSchemaSQLLineBytes {
			return 0, s.abort(ctx)
		}
		s.line = append(s.line, value)
	}
	return len(data), nil
}

func (s *publicSchemaTransformSink) writeLine(ctx context.Context, newline bool) error {
	line := s.line
	transform := s.lex.normal() && bytes.Equal(line, []byte("CREATE SCHEMA public;"))
	if transform {
		s.replacements++
		if s.replacements != 1 {
			return s.abort(ctx)
		}
		line = []byte("CREATE SCHEMA IF NOT EXISTS public;")
	}
	output := append([]byte(nil), line...)
	if newline {
		output = append(output, '\n')
	}
	written, err := s.downstream.WriteContext(ctx, output)
	if err != nil || written != len(output) {
		return s.abort(ctx)
	}
	s.lex.scan(line, newline)
	return nil
}

func (s *publicSchemaTransformSink) CloseContext(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.err
	}
	if ctx == nil || ctx.Err() != nil || s.downstream == nil {
		return s.abort(ctx)
	}
	if len(s.line) != 0 {
		if err := s.writeLine(ctx, false); err != nil {
			return err
		}
		s.line = nil
	}
	if s.replacements != 1 || !s.lex.complete() {
		return s.abort(ctx)
	}
	s.closed = true
	s.err = s.downstream.CloseContext(ctx)
	return s.err
}

func (s *publicSchemaTransformSink) AbortContext(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.err
	}
	return s.abort(ctx)
}

func (s *publicSchemaTransformSink) abort(ctx context.Context) error {
	s.closed = true
	s.err = tools.ErrOutput
	if s.downstream != nil {
		_ = s.downstream.AbortContext(ctx)
	}
	return s.err
}

// pgSQLLexState keeps the transform out of comments, strings, identifiers, and function bodies.
type pgSQLLexState struct {
	singleQuote   bool
	singleEscape  bool
	doubleQuote   bool
	lineComment   bool
	blockComments int
	dollarTag     []byte
}

func (s pgSQLLexState) normal() bool {
	return !s.singleQuote && !s.doubleQuote && !s.lineComment && s.blockComments == 0 && len(s.dollarTag) == 0
}

func (s pgSQLLexState) complete() bool {
	return !s.singleQuote && !s.doubleQuote && s.blockComments == 0 && len(s.dollarTag) == 0
}

func (s *pgSQLLexState) scan(line []byte, newline bool) {
	for index := 0; index < len(line); {
		switch {
		case s.blockComments > 0:
			if index+1 < len(line) && line[index] == '/' && line[index+1] == '*' {
				s.blockComments++
				index += 2
			} else if index+1 < len(line) && line[index] == '*' && line[index+1] == '/' {
				s.blockComments--
				index += 2
			} else {
				index++
			}
		case s.lineComment:
			index = len(line)
		case s.singleQuote:
			if s.singleEscape && line[index] == '\\' {
				index += 2
			} else if line[index] == '\'' && index+1 < len(line) && line[index+1] == '\'' {
				index += 2
			} else if line[index] == '\'' {
				s.singleQuote, s.singleEscape = false, false
				index++
			} else {
				index++
			}
		case s.doubleQuote:
			if line[index] == '"' && index+1 < len(line) && line[index+1] == '"' {
				index += 2
			} else if line[index] == '"' {
				s.doubleQuote = false
				index++
			} else {
				index++
			}
		case len(s.dollarTag) != 0:
			if bytes.HasPrefix(line[index:], s.dollarTag) {
				index += len(s.dollarTag)
				s.dollarTag = nil
			} else {
				index++
			}
		case index+1 < len(line) && line[index] == '-' && line[index+1] == '-':
			s.lineComment = true
			index = len(line)
		case index+1 < len(line) && line[index] == '/' && line[index+1] == '*':
			s.blockComments = 1
			index += 2
		case line[index] == '\'':
			s.singleQuote = true
			s.singleEscape = index > 0 && (line[index-1] == 'e' || line[index-1] == 'E') && (index < 2 || !sqlIdentifierByte(line[index-2]))
			index++
		case line[index] == '"':
			s.doubleQuote = true
			index++
		case line[index] == '$':
			if tag := pgDollarTag(line[index:]); len(tag) != 0 {
				s.dollarTag = tag
				index += len(tag)
			} else {
				index++
			}
		default:
			index++
		}
	}
	if newline && s.lineComment {
		s.lineComment = false
	}
}

func pgDollarTag(value []byte) []byte {
	if len(value) < 2 || value[0] != '$' {
		return nil
	}
	for index := 1; index < len(value); index++ {
		if value[index] == '$' {
			return append([]byte(nil), value[:index+1]...)
		}
		if !sqlIdentifierByte(value[index]) || index == 1 && value[index] >= '0' && value[index] <= '9' {
			return nil
		}
	}
	return nil
}

func sqlIdentifierByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_'
}

var _ tools.AbortableOutputSink = (*publicSchemaTransformSink)(nil)
