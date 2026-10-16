package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRetryExhaustedErrorPreservesRetryMetadataWithoutContent(t *testing.T) {
	secret := errors.New("provider returned 429; prompt=private-document-content")
	err := &RetryExhaustedError{Attempts: 2, RetryAfter: 7 * time.Second, Err: secret}
	if got := err.Error(); got != "all retry attempts failed after 2 attempts" {
		t.Fatalf("unexpected safe error: %q", got)
	}
	if !errors.Is(err, secret) {
		t.Fatal("wrapped provider error was not preserved")
	}
	if strings.Contains(err.Error(), "private-document-content") {
		t.Fatal("safe error exposed prompt content")
	}
}

func TestRetryableLLMFailureOnlyTransientClasses(t *testing.T) {
	for _, tc := range []struct {
		name, msg string
		want      bool
	}{
		{"rate limit", "HTTP 429 rate limit", true},
		{"service unavailable", "HTTP 503 service unavailable", true},
		{"timeout", "context deadline exceeded", true},
		{"auth", "HTTP 401 unauthorized", false},
		{"budget", "monthly budget exceeded", false},
		{"parse", "invalid JSON response", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryableLLMFailure(errors.New(tc.msg)); got != tc.want {
				t.Fatalf("retryableLLMFailure(%q)=%v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

func TestWorkflowStepErrorSupportsRetryClassification(t *testing.T) {
	inner := &RetryExhaustedError{Attempts: 2, RetryAfter: 3 * time.Second, Err: errors.New("HTTP 429")}
	err := &WorkflowStepError{Step: "custom_fields", Err: inner}
	var got *RetryExhaustedError
	if !errors.As(err, &got) || got.Attempts != 2 {
		t.Fatalf("retry metadata was not discoverable through step wrapper: %#v", err)
	}
	if err.Error() != "workflow step custom_fields failed" {
		t.Fatalf("unsafe step error: %q", err.Error())
	}
}
