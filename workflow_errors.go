package main

import (
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
	// Explicit permanent classes win before the bounded transient fallbacks;
	// this prevents an incidental number in an auth/request identifier from
	// turning a non-retryable response into a retry loop.
	for _, marker := range []string{"400", "401", "403", "404", "422", "unauthorized", "forbidden", "invalid api key", "authentication", "permission denied", "budget exceeded", "invalid request", "bad request", "invalid json", "parse error"} {
		if strings.Contains(s, marker) {
			return false
		}
	}
	for _, marker := range []string{"http 429", "status code: 429", "429 too", "rate limit", "rate_limit", "too many requests", "http 503", "status code: 503", "503 service", "service unavailable", "timeout", "deadline exceeded", "connection reset", "temporarily unavailable"} {
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
