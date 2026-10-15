package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestRequestCorrelationMiddlewarePreservesSafeID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requestCorrelationMiddleware())
	router.GET("/test", func(c *gin.Context) {
		require.Equal(t, "safe.request-1", requestIDFromContext(c.Request.Context()))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(requestIDHeader, "safe.request-1")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	require.Equal(t, http.StatusNoContent, res.Code)
	require.Equal(t, "safe.request-1", res.Header().Get(requestIDHeader))
}

func TestRequestCorrelationMiddlewarePreservesValidTraceparent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requestCorrelationMiddleware())
	router.GET("/test", func(c *gin.Context) {
		traceparent := traceparentFromContext(c.Request.Context())
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, "http://paperless.test", nil)
		require.NoError(t, err)
		decorateOutboundRequest(req)
		require.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", traceparent)
		require.Equal(t, "safe.request-1", req.Header.Get(requestIDHeader))
		require.Equal(t, traceparent, req.Header.Get(traceparentHeader))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(requestIDHeader, "safe.request-1")
	req.Header.Set(traceparentHeader, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	require.Equal(t, http.StatusNoContent, res.Code)
}

func TestTraceparentFromContextRejectsInvalidValues(t *testing.T) {
	ctx := context.WithValue(context.Background(), traceparentContextKey{}, "00-00000000000000000000000000000000-00f067aa0ba902b7-01")
	require.Empty(t, traceparentFromContext(ctx))
	ctx = context.WithValue(context.Background(), traceparentContextKey{}, "not-a-traceparent")
	require.Empty(t, traceparentFromContext(ctx))
	ctx = context.WithValue(context.Background(), traceparentContextKey{}, "ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	require.Empty(t, traceparentFromContext(ctx))
}

func TestRequestCorrelationLogUsesRouteWithoutSensitivePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	logger := logrus.StandardLogger()
	previousOut, previousLevel, previousFormatter := logger.Out, logger.Level, logger.Formatter
	logger.SetOutput(&output)
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	t.Cleanup(func() {
		logger.SetOutput(previousOut)
		logger.SetLevel(previousLevel)
		logger.SetFormatter(previousFormatter)
	})

	router := gin.New()
	router.Use(requestCorrelationMiddleware())
	router.GET("/documents/:secret", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	secret := "private-document-title"
	req := httptest.NewRequest(http.MethodGet, "/documents/"+secret, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	require.Equal(t, http.StatusNoContent, res.Code)
	require.Contains(t, output.String(), `route="/documents/:secret"`)
	require.NotContains(t, output.String(), secret)
}

func TestRequestCorrelationMiddlewareReplacesUnsafeID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requestCorrelationMiddleware())
	router.GET("/test", func(c *gin.Context) {
		id := requestIDFromContext(c.Request.Context())
		require.Regexp(t, requestIDPattern, id)
		require.NotEqual(t, "bad id with spaces", id)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(requestIDHeader, "bad id with spaces")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	require.Equal(t, http.StatusNoContent, res.Code)
	require.Regexp(t, requestIDPattern, res.Header().Get(requestIDHeader))
}
