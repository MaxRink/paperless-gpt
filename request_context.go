package main

import (
	"context"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	logrus "github.com/sirupsen/logrus"
	"paperless-gpt/ocr"
)

const requestIDHeader = "X-Request-ID"
const traceparentHeader = "traceparent"

type requestIDContextKey struct{}
type traceparentContextKey struct{}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
var traceparentPattern = regexp.MustCompile(`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

// requestCorrelationMiddleware keeps a bounded correlation identifier on the
// request context and response. Invalid caller values are replaced rather
// than copied into logs. The identifier is metadata only; request payloads,
// credentials and response bodies are never logged here.
func requestCorrelationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(requestIDHeader)
		if !requestIDPattern.MatchString(requestID) {
			requestID = uuid.NewString()
		}
		ctx := context.WithValue(c.Request.Context(), requestIDContextKey{}, requestID)
		if traceparent := c.GetHeader(traceparentHeader); validTraceparent(traceparent) {
			ctx = context.WithValue(ctx, traceparentContextKey{}, traceparent)
		}
		ctx = ocr.WithRequestCorrelation(ctx, requestID, traceparentFromContext(ctx))
		c.Request = c.Request.WithContext(ctx)
		c.Set(requestIDHeader, requestID)
		c.Header(requestIDHeader, requestID)

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		fields := logrus.Fields{
			"request_id": requestID,
			"method":     c.Request.Method,
			"route":      route,
			"status":     c.Writer.Status(),
		}
		if traceparent := traceparentFromContext(ctx); traceparent != "" {
			fields[traceparentHeader] = traceparent
		}
		logrus.WithFields(fields).Info("HTTP request completed")
	}
}

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

func traceparentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	traceparent, _ := ctx.Value(traceparentContextKey{}).(string)
	if !validTraceparent(traceparent) {
		return ""
	}
	return traceparent
}

func validTraceparent(value string) bool {
	return traceparentPattern.MatchString(value) && value[:2] != "ff" && value[3:35] != "00000000000000000000000000000000" && value[36:52] != "0000000000000000"
}

func decorateOutboundRequest(req *http.Request) {
	if req == nil {
		return
	}
	if requestID := requestIDFromContext(req.Context()); requestID != "" {
		req.Header.Set(requestIDHeader, requestID)
	}
	if traceparent := traceparentFromContext(req.Context()); traceparent != "" {
		req.Header.Set(traceparentHeader, traceparent)
	}
}

func requestLogger(ctx context.Context) *logrus.Entry {
	entry := logrus.WithContext(ctx)
	if requestID := requestIDFromContext(ctx); requestID != "" {
		entry = entry.WithField("request_id", requestID)
	}
	if traceparent := traceparentFromContext(ctx); traceparent != "" {
		entry = entry.WithField(traceparentHeader, traceparent)
	}
	return entry
}
