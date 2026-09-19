package httputil

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"testing/iotest"

	"github.com/klauspost/compress/zstd"
)

type bodyReaderOnly struct{ io.Reader }

func TestReadRequestBodyChunksPreservesContent(t *testing.T) {
	for _, size := range []int{0, 1, 512, 513, 1 << 20, (2 << 20) + 17} {
		body := bytes.Repeat([]byte{'x'}, size)
		for _, declared := range []int64{-1, 0, 1, int64(size), 1 << 40} {
			t.Run(fmt.Sprintf("size_%d_length_%d", size, declared), func(t *testing.T) {
				req := &http.Request{Body: io.NopCloser(bodyReaderOnly{bytes.NewReader(body)}), ContentLength: declared, Header: make(http.Header)}
				got, err := ReadRequestBodyWithPrealloc(req)
				if err != nil || !bytes.Equal(got, body) {
					t.Fatalf("body was lost or changed: %v", err)
				}
			})
		}
	}
}

func TestReadRequestBodyChunksPreservesReadErrors(t *testing.T) {
	for _, readErr := range []error{io.ErrUnexpectedEOF, errors.New("connection reset")} {
		reader := io.MultiReader(bytes.NewReader([]byte("partial")), iotest.ErrReader(readErr))
		req := &http.Request{Body: io.NopCloser(reader), ContentLength: 100, Header: make(http.Header)}
		got, err := ReadRequestBodyWithPrealloc(req)
		if !errors.Is(err, readErr) || got != nil {
			t.Fatalf("truncated body accepted: body=%q err=%v", got, err)
		}
	}
}

func TestReadRequestBodyChunksPreservesMaxBytesReader(t *testing.T) {
	body := bytes.Repeat([]byte{'x'}, (2<<20)+1)
	req := &http.Request{Body: http.MaxBytesReader(nil, io.NopCloser(bytes.NewReader(body)), 2<<20), ContentLength: int64(len(body)), Header: make(http.Header)}
	got, err := ReadRequestBodyWithPrealloc(req)
	var limitErr *http.MaxBytesError
	if !errors.As(err, &limitErr) || limitErr.Limit != 2<<20 || got != nil {
		t.Fatalf("body limit changed: len=%d err=%v", len(got), err)
	}
}

func BenchmarkReadLargeBody(b *testing.B) {
	for _, size := range []int{512 << 10, 69 << 20} {
		b.Run(fmt.Sprintf("bytes_%d", size), func(b *testing.B) {
			body := bytes.Repeat([]byte{'x'}, size)
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req := &http.Request{Body: io.NopCloser(bodyReaderOnly{bytes.NewReader(body)}), ContentLength: int64(size), Header: make(http.Header)}
				got, err := ReadRequestBodyWithPrealloc(req)
				if err != nil || !bytes.Equal(got, body) {
					b.Fatalf("body changed: %v", err)
				}
			}
		})
	}

	// 3.5 MB realistic JSON conversation history payload
	targetSize := int(3.5 * 1024 * 1024)
	pattern := []byte(`{"role":"user","content":"repeat conversation context for testing payload ingress speed"}`)
	repeatCount := targetSize / len(pattern)
	var rawJSONBuf bytes.Buffer
	rawJSONBuf.WriteString(`{"model":"gemini-3.8-flash-tiered","messages":[`)
	for i := 0; i < repeatCount; i++ {
		if i > 0 {
			rawJSONBuf.WriteByte(',')
		}
		rawJSONBuf.Write(pattern)
	}
	rawJSONBuf.WriteString(`]}`)
	rawJSON := rawJSONBuf.Bytes()

	// Gzip pre-compression
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write(rawJSON)
	_ = gw.Close()
	gzBytes := gzBuf.Bytes()

	// Zstd pre-compression
	zstdWriter, _ := zstd.NewWriter(nil)
	zstdBytes := zstdWriter.EncodeAll(rawJSON, nil)
	_ = zstdWriter.Close()

	b.Run("3.5MB_identity", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(rawJSON)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req := &http.Request{Body: io.NopCloser(bytes.NewReader(rawJSON)), ContentLength: int64(len(rawJSON)), Header: make(http.Header)}
			got, err := ReadRequestBodyWithPrealloc(req)
			if err != nil || len(got) != len(rawJSON) {
				b.Fatalf("failed read: %v", err)
			}
		}
	})

	b.Run("3.5MB_gzip", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(rawJSON)))
		b.ReportMetric(float64(len(gzBytes)), "wire-bytes/op")
		b.ReportMetric(float64(len(rawJSON)), "decoded-bytes/op")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			hdr := make(http.Header)
			hdr.Set("Content-Encoding", "gzip")
			req := &http.Request{Body: io.NopCloser(bytes.NewReader(gzBytes)), ContentLength: int64(len(gzBytes)), Header: hdr}
			got, err := ReadRequestBodyWithPrealloc(req)
			if err != nil || len(got) != len(rawJSON) {
				b.Fatalf("failed read: %v", err)
			}
		}
	})

	b.Run("3.5MB_zstd", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(rawJSON)))
		b.ReportMetric(float64(len(zstdBytes)), "wire-bytes/op")
		b.ReportMetric(float64(len(rawJSON)), "decoded-bytes/op")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			hdr := make(http.Header)
			hdr.Set("Content-Encoding", "zstd")
			req := &http.Request{Body: io.NopCloser(bytes.NewReader(zstdBytes)), ContentLength: int64(len(zstdBytes)), Header: hdr}
			got, err := ReadRequestBodyWithPrealloc(req)
			if err != nil || len(got) != len(rawJSON) {
				b.Fatalf("failed read: %v", err)
			}
		}
	})
}
