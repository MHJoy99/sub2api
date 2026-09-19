package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"net/http"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

const samplePayload = `{"model":"gpt-5.5","input":"hi","stream":false}`

func newRequestWithBody(t *testing.T, body []byte, encoding string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	req.ContentLength = int64(len(body))
	return req
}

func TestReadRequestBodyWithPrealloc_PassesThroughIdentity(t *testing.T) {
	req := newRequestWithBody(t, []byte(samplePayload), "")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}

func TestReadRequestBodyWithPrealloc_DecodesZstd(t *testing.T) {
	enc, _ := zstd.NewWriter(nil)
	compressed := enc.EncodeAll([]byte(samplePayload), nil)
	_ = enc.Close()

	req := newRequestWithBody(t, compressed, "zstd")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
	if req.Header.Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding should be cleared after decoding")
	}
	if req.ContentLength != int64(len(samplePayload)) {
		t.Fatalf("ContentLength not updated: %d", req.ContentLength)
	}
}

func TestReadRequestBodyWithPrealloc_DecodesGzip(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte(samplePayload)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	req := newRequestWithBody(t, buf.Bytes(), "gzip")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}

func TestReadRequestBodyWithPrealloc_DecodesDeflate(t *testing.T) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write([]byte(samplePayload)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	req := newRequestWithBody(t, buf.Bytes(), "deflate")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}

func TestReadRequestBodyWithPrealloc_RejectsUnsupportedEncoding(t *testing.T) {
	req := newRequestWithBody(t, []byte(samplePayload), "br")
	_, err := ReadRequestBodyWithPrealloc(req)
	if err == nil {
		t.Fatal("expected error for unsupported encoding, got nil")
	}
	if !strings.Contains(err.Error(), "br") {
		t.Fatalf("error should mention encoding, got %v", err)
	}
}

func TestReadRequestBodyWithPrealloc_RejectsCorruptZstd(t *testing.T) {
	req := newRequestWithBody(t, []byte("not actually zstd"), "zstd")
	_, err := ReadRequestBodyWithPrealloc(req)
	if err == nil {
		t.Fatal("expected error for corrupt zstd body, got nil")
	}
}

func TestReadRequestBodyWithPrealloc_NilBody(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/v1/responses", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil body, got %q", got)
	}
}

func TestReadRequestBodyWithPrealloc_RespectsIdentityEncoding(t *testing.T) {
	req := newRequestWithBody(t, []byte(samplePayload), "identity")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}
func TestRequestBodyReadMetrics_IdentityAndCompressed(t *testing.T) {
	type testCase struct {
		name       string
		encoding   string
		compressFn func([]byte) []byte
	}
	cases := []testCase{
		{
			name:     "identity",
			encoding: "",
			compressFn: func(b []byte) []byte {
				return b
			},
		},
		{
			name:     "gzip",
			encoding: "gzip",
			compressFn: func(b []byte) []byte {
				var buf bytes.Buffer
				gw := gzip.NewWriter(&buf)
				_, _ = gw.Write(b)
				_ = gw.Close()
				return buf.Bytes()
			},
		},
		{
			name:     "zstd",
			encoding: "zstd",
			compressFn: func(b []byte) []byte {
				enc, _ := zstd.NewWriter(nil)
				out := enc.EncodeAll(b, nil)
				_ = enc.Close()
				return out
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := tc.compressFn([]byte(samplePayload))
			req := newRequestWithBody(t, wire, tc.encoding)
			ctx := WithRequestBodyReadMetrics(req.Context())
			req = req.WithContext(ctx)

			got, err := ReadRequestBodyWithPrealloc(req)
			if err != nil {
				t.Fatalf("ReadRequestBodyWithPrealloc: %v", err)
			}
			if string(got) != samplePayload {
				t.Fatalf("body mismatch: got %q, want %q", got, samplePayload)
			}

			snap, ok := RequestBodyReadMetricsFromContext(req.Context())
			if !ok {
				t.Fatalf("expected metrics snapshot to be present")
			}
			wantEncoding := tc.encoding
			if wantEncoding == "" {
				wantEncoding = "identity"
			}
			if snap.ContentEncoding != wantEncoding {
				t.Fatalf("ContentEncoding mismatch: got %q, want %q", snap.ContentEncoding, wantEncoding)
			}
			if snap.DeclaredWireBytes != int64(len(wire)) {
				t.Fatalf("DeclaredWireBytes mismatch: got %d, want %d", snap.DeclaredWireBytes, len(wire))
			}
			if snap.ActualWireBytes != int64(len(wire)) {
				t.Fatalf("ActualWireBytes mismatch: got %d, want %d", snap.ActualWireBytes, len(wire))
			}
			if snap.DecompressedBytes != int64(len(samplePayload)) {
				t.Fatalf("DecompressedBytes mismatch: got %d, want %d", snap.DecompressedBytes, len(samplePayload))
			}
			if snap.WireReadDuration < 0 {
				t.Fatalf("invalid WireReadDuration: %v", snap.WireReadDuration)
			}
			if snap.DecodeDuration < 0 {
				t.Fatalf("invalid DecodeDuration: %v", snap.DecodeDuration)
			}

			// PrereadBody pass-through should not overwrite the snapshot
			req.Body = NewPrereadBody(got)
			gotSecond, err := ReadRequestBodyWithPrealloc(req)
			if err != nil {
				t.Fatalf("second read failed: %v", err)
			}
			if !bytes.Equal(gotSecond, got) {
				t.Fatalf("second read body altered")
			}
			snapSecond, ok := RequestBodyReadMetricsFromContext(req.Context())
			if !ok || snapSecond != snap {
				t.Fatalf("snapshot changed after second read: before=%+v, after=%+v", snap, snapSecond)
			}
		})
	}
}
