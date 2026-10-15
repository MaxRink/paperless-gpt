package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// RetryExhaustedError is safe to expose in a workflow response. It contains
// no prompt or provider response body, only retry timing and the wrapped cause.
type RetryExhaustedError struct {
	Attempts   int
	RetryAfter time.Duration
	Err        error
}

func (e *RetryExhaustedError) Error() string {
	return fmt.Sprintf("all retry attempts failed after %d attempts", e.Attempts)
}

func (e *RetryExhaustedError) Unwrap() error { return e.Err }

// retryableLLMFailure deliberately covers only transient classes. Auth,
// budget, parse and permission errors must remain ordinary failures.
func retryableLLMFailure(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, marker := range []string{"429", "rate limit", "rate_limit", "too many requests", "503", "service unavailable", "timeout", "deadline exceeded", "connection reset", "temporarily unavailable"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// WorkflowStepError identifies the failed generator without exposing content.
type WorkflowStepError struct {
	Step string
	Err  error
}

func (e *WorkflowStepError) Error() string { return "workflow step " + e.Step + " failed" }
func (e *WorkflowStepError) Unwrap() error { return e.Err }

func retryInfo(err error) (*RetryExhaustedError, bool) {
	var r *RetryExhaustedError
	return r, errors.As(err, &r)
}
