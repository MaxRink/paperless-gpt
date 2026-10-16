package ocr

import "context"

type requestCorrelationContextKey struct{}

type requestCorrelation struct {
	requestID   string
	traceparent string
}

// WithRequestCorrelation attaches sanitized request metadata to a context so
// provider transports can correlate calls without receiving document content.
func WithRequestCorrelation(ctx context.Context, requestID, traceparent string) context.Context {
	return context.WithValue(ctx, requestCorrelationContextKey{}, requestCorrelation{
		requestID:   requestID,
		traceparent: traceparent,
	})
}

func requestCorrelationFromContext(ctx context.Context) (requestCorrelation, bool) {
	correlation, ok := ctx.Value(requestCorrelationContextKey{}).(requestCorrelation)
	return correlation, ok
}
