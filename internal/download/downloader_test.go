package download_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"goravel/internal/download"
	// See app/support/imageconfig's test file for why this needs to be an
	// external ("_test") package.
	_ "goravel/tests"
)

func TestDownload_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("fake-image-bytes"))
	}))
	defer srv.Close()

	result, err := download.Download(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	defer os.Remove(result.Path)

	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "fake-image-bytes" {
		t.Errorf("downloaded content = %q, want %q", data, "fake-image-bytes")
	}
}

func TestDownload_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := download.Download(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected an error for a 404 response, got nil")
	}
}

func TestDownload_RejectsNonHTTPScheme(t *testing.T) {
	_, err := download.Download(context.Background(), "ftp://example.com/image.jpg")
	if err != download.ErrUnsupportedScheme {
		t.Fatalf("err = %v, want ErrUnsupportedScheme", err)
	}
}

func TestDownload_OversizedBodyRejected(t *testing.T) {
	// MAX_SOURCE_DOWNLOAD_SIZE defaults to 25MB; stream well past it without
	// declaring Content-Length so the size check has to happen mid-stream.
	body := strings.Repeat("a", 26*1024*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fw, ok := w.(http.Flusher)
		_, _ = w.Write([]byte(body[:1024]))
		if ok {
			fw.Flush()
		}
		_, _ = w.Write([]byte(body[1024:]))
	}))
	defer srv.Close()

	_, err := download.Download(context.Background(), srv.URL)
	if err != download.ErrResponseTooLarge {
		t.Fatalf("err = %v, want ErrResponseTooLarge", err)
	}
}

func TestDownload_TooManyRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
	}))
	defer srv.Close()

	_, err := download.Download(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected a too-many-redirects error, got nil")
	}
}
