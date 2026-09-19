package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

type testLogSink struct {
	mu     sync.Mutex
	events []*logger.LogEvent
}

func (s *testLogSink) WriteLogEvent(event *logger.LogEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *testLogSink) list() []*logger.LogEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*logger.LogEvent, len(s.events))
	copy(out, s.events)
	return out
}

func initMiddlewareTestLogger(t *testing.T) *testLogSink {
	return initMiddlewareTestLoggerWithLevel(t, "debug")
}

func initMiddlewareTestLoggerWithLevel(t *testing.T, level string) *testLogSink {
	t.Helper()
	level = strings.TrimSpace(level)
	if level == "" {
		level = "debug"
	}
	if err := logger.Init(logger.InitOptions{
		Level:       level,
		Format:      "json",
		ServiceName: "sub2api",
		Environment: "test",
		Output: logger.OutputOptions{
			ToStdout: false,
			ToFile:   false,
		},
	}); err != nil {
		t.Fatalf("init logger: %v", err)
	}
	sink := &testLogSink{}
	logger.SetSink(sink)
	t.Cleanup(func() {
		logger.SetSink(nil)
	})
	return sink
}

func TestRequestLogger_GenerateAndPropagateRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestLogger())
	r.GET("/t", func(c *gin.Context) {
		reqID, ok := c.Request.Context().Value(ctxkey.RequestID).(string)
		if !ok || reqID == "" {
			t.Fatalf("request_id missing in context")
		}
		if got := c.Writer.Header().Get(requestIDHeader); got != reqID {
			t.Fatalf("response header request_id mismatch, header=%q ctx=%q", got, reqID)
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if w.Header().Get(requestIDHeader) == "" {
		t.Fatalf("X-Request-ID should be set")
	}
}

func TestRequestLogger_KeepIncomingRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestLogger())
	r.GET("/t", func(c *gin.Context) {
		reqID, _ := c.Request.Context().Value(ctxkey.RequestID).(string)
		if reqID != "rid-fixed" {
			t.Fatalf("request_id=%q, want rid-fixed", reqID)
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set(requestIDHeader, "rid-fixed")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if got := w.Header().Get(requestIDHeader); got != "rid-fixed" {
		t.Fatalf("header=%q, want rid-fixed", got)
	}
}

func TestRequestLoggerBoundsIncomingRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestLogger())
	r.GET("/t", func(c *gin.Context) {
		reqID, _ := c.Request.Context().Value(ctxkey.RequestID).(string)
		if len(reqID) != 36 {
			t.Fatalf("request_id length=%d", len(reqID))
		}
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set(requestIDHeader, strings.Repeat("r", 1024))
	r.ServeHTTP(w, req)
	if got := len(w.Header().Get(requestIDHeader)); got != 36 {
		t.Fatalf("response request_id length=%d", got)
	}
}

func TestLogger_AccessLogIncludesCoreFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)

	r := gin.New()
	r.Use(Logger())
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, ctxkey.AccountID, int64(101))
		ctx = context.WithValue(ctx, ctxkey.Platform, "openai")
		ctx = context.WithValue(ctx, ctxkey.Model, "gpt-5")
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.GET("/api/test", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d", w.Code)
	}

	events := sink.list()
	if len(events) == 0 {
		t.Fatalf("expected at least one log event")
	}
	found := false
	for _, event := range events {
		if event == nil || event.Message != "http request completed" {
			continue
		}
		found = true
		switch v := event.Fields["status_code"].(type) {
		case int:
			if v != http.StatusCreated {
				t.Fatalf("status_code field mismatch: %v", v)
			}
		case int64:
			if v != int64(http.StatusCreated) {
				t.Fatalf("status_code field mismatch: %v", v)
			}
		default:
			t.Fatalf("status_code type mismatch: %T", v)
		}
		switch v := event.Fields["account_id"].(type) {
		case int64:
			if v != 101 {
				t.Fatalf("account_id field mismatch: %v", v)
			}
		case int:
			if v != 101 {
				t.Fatalf("account_id field mismatch: %v", v)
			}
		default:
			t.Fatalf("account_id type mismatch: %T", v)
		}
		if event.Fields["platform"] != "openai" || event.Fields["model"] != "gpt-5" {
			t.Fatalf("platform/model mismatch: %+v", event.Fields)
		}
	}
	if !found {
		t.Fatalf("access log event not found")
	}
}

func TestLogger_IngressRejectRemainsInStandardAccessLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)
	r := gin.New()
	r.Use(Logger())
	r.GET("/v1/messages", func(c *gin.Context) {
		MarkIngressRejected(c, IngressRejectInvalidAPIKey)
		c.Status(http.StatusUnauthorized)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
	events := sink.list()
	if len(events) != 1 {
		t.Fatalf("events=%d, want 1", len(events))
	}
	if got := events[0].Fields["ingress_reject_reason"]; got != string(IngressRejectInvalidAPIKey) {
		t.Fatalf("ingress_reject_reason=%v", got)
	}
	if got, _ := events[0].Fields[logger.OpsSystemLogSkipField].(bool); !got {
		t.Fatalf("%s must be true", logger.OpsSystemLogSkipField)
	}
}

func TestLogger_AccessLogUsesForwardedClientIPFromTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)

	r := gin.New()
	if err := r.SetTrustedProxies([]string{"104.23.251.120"}); err != nil {
		t.Fatalf("set trusted proxies: %v", err)
	}
	r.Use(Logger())
	r.GET("/api/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.RemoteAddr = "104.23.251.120:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}

	for _, event := range sink.list() {
		if event == nil || event.Message != "http request completed" {
			continue
		}
		if got := event.Fields["client_ip"]; got != "203.0.113.42" {
			t.Fatalf("client_ip=%q, want real forwarded ip", got)
		}
		return
	}
	t.Fatalf("access log event not found")
}

func TestLogger_HealthPathSkipped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)

	r := gin.New()
	r.Use(Logger())
	r.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if len(sink.list()) != 0 {
		t.Fatalf("health endpoint should not write access log")
	}
}

func TestLogger_AccessLogDroppedWhenLevelWarn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLoggerWithLevel(t, "warn")

	r := gin.New()
	r.Use(RequestLogger())
	r.Use(Logger())
	r.GET("/api/test", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d", w.Code)
	}

	events := sink.list()
	for _, event := range events {
		if event != nil && event.Message == "http request completed" {
			t.Fatalf("access log should not be indexed when level=warn: %+v", event)
		}
	}
}
func TestLogger_AccessLogIncludesRequestBodyMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)

	r := gin.New()
	r.Use(RequestLogger())
	r.Use(Logger())

	r.POST("/api/echo", func(c *gin.Context) {
		body, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		c.Data(http.StatusOK, "application/json", body)
	})

	const secretMarker = "sensitive-payload-xyz-987"
	payload := []byte(`{"message":"` + secretMarker + `"}`)

	// Case 1: identity
	{
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/echo", bytes.NewReader(payload))
		req.ContentLength = int64(len(payload))
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
		}
	}

	// Case 2: gzip
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write(payload)
	_ = gw.Close()
	gzBytes := gzBuf.Bytes()

	{
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/echo", bytes.NewReader(gzBytes))
		req.Header.Set("Content-Encoding", "gzip")
		req.ContentLength = int64(len(gzBytes))
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
		}
	}

	events := sink.list()
	var identityEvent, gzipEvent *logger.LogEvent
	for _, ev := range events {
		if ev.Message == "http request completed" && ev.Fields["path"] == "/api/echo" {
			if ev.Fields["request_content_encoding"] == "identity" {
				identityEvent = ev
			} else if ev.Fields["request_content_encoding"] == "gzip" {
				gzipEvent = ev
			}
		}
	}

	if identityEvent == nil {
		t.Fatalf("identity access log event not found")
	}
	if gzipEvent == nil {
		t.Fatalf("gzip access log event not found")
	}

	// Verify identity fields
	if identityEvent.Fields["request_wire_bytes"] != int64(len(payload)) {
		t.Fatalf("identity request_wire_bytes mismatch: got %v, want %d", identityEvent.Fields["request_wire_bytes"], len(payload))
	}
	if identityEvent.Fields["request_decompressed_bytes"] != int64(len(payload)) {
		t.Fatalf("identity request_decompressed_bytes mismatch: got %v, want %d", identityEvent.Fields["request_decompressed_bytes"], len(payload))
	}

	// Verify gzip fields
	if gzipEvent.Fields["request_wire_bytes"] != int64(len(gzBytes)) {
		t.Fatalf("gzip request_wire_bytes mismatch: got %v, want %d", gzipEvent.Fields["request_wire_bytes"], len(gzBytes))
	}
	if gzipEvent.Fields["request_decompressed_bytes"] != int64(len(payload)) {
		t.Fatalf("gzip request_decompressed_bytes mismatch: got %v, want %d", gzipEvent.Fields["request_decompressed_bytes"], len(payload))
	}

	// Verify no sensitive payload leaked
	for _, ev := range []*logger.LogEvent{identityEvent, gzipEvent} {
		for k, v := range ev.Fields {
			strVal := fmt.Sprintf("%v", v)
			if strings.Contains(k, secretMarker) || strings.Contains(strVal, secretMarker) {
				t.Fatalf("sensitive marker leaked in field %s: %s", k, strVal)
			}
		}
	}
}
