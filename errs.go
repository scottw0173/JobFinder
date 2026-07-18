package main

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime"
)

// stackTracer is implemented by errors that carry a captured call stack.
type stackTracer interface {
	StackTrace() []string
}

type tracedError struct {
	err   error
	stack []uintptr
}

func (e *tracedError) Error() string { return e.err.Error() }
func (e *tracedError) Unwrap() error { return e.err }

func (e *tracedError) StackTrace() []string {
	frames := runtime.CallersFrames(e.stack)
	out := make([]string, 0, len(e.stack))
	for {
		frame, more := frames.Next()
		out = append(out, fmt.Sprintf("%s\n\t%s:%d", frame.Function, frame.File, frame.Line))
		if !more {
			break
		}
	}
	return out
}

func hasStack(err error) bool {
	var st stackTracer
	return errors.As(err, &st)
}

// captureStack records the call stack starting at its own caller. Each of
// WithStack/wrapErr/traceErrorf/errAttr must call it directly (not through
// each other) so the skip count always lands on the site that mattered -
// the caller of whichever one of them was actually invoked.
func captureStack() []uintptr {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(3, pcs)
	return pcs[:n]
}

// WithStack attaches a call stack to err, unless err's chain already carries
// one - so repeated wrapping on the way up the call stack doesn't overwrite
// the trace pointing at the original failure.
func WithStack(err error) error {
	if err == nil || hasStack(err) {
		return err
	}
	return &tracedError{err: err, stack: captureStack()}
}

// wrapErr adds context to err via fmt.Errorf("%w") and ensures the chain
// carries a stack trace, captured here if the error doesn't have one yet.
func wrapErr(msg string, err error) error {
	if err != nil && !hasStack(err) {
		err = &tracedError{err: err, stack: captureStack()}
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// traceErrorf builds a new error (no existing error to wrap) and captures a
// stack trace at the call site.
func traceErrorf(format string, args ...any) error {
	return &tracedError{err: fmt.Errorf(format, args...), stack: captureStack()}
}

// errAttr renders err for structured logging as an "error" group containing
// the full wrapped message chain and, when available, the stack trace
// captured at the error's origin.
func errAttr(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	if !hasStack(err) {
		err = &tracedError{err: err, stack: captureStack()}
	}
	attrs := []any{slog.String("message", err.Error())}
	var st stackTracer
	if errors.As(err, &st) {
		attrs = append(attrs, slog.Any("stack", st.StackTrace()))
	}
	return slog.Group("error", attrs...)
}
