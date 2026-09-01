// Package download fetches a client-supplied source image URL to a local
// temporary file under strict, configurable limits.
//
// SECURITY NOTE - SSRF is explicitly NOT handled here. Per an explicit
// product decision, this deployment does not restrict which hosts it will
// fetch from: it will happily request http://127.0.0.1, RFC1918 addresses,
// the cloud metadata endpoint (169.254.169.254), etc. if given a URL that
// resolves there. Do not expose this service to untrusted clients or the
// public internet without adding host/IP allow-listing here first (resolve
// the host, reject private/loopback/link-local/metadata ranges, dial the
// resolved IP directly to avoid DNS-rebinding, and re-validate on every
// redirect hop). See README.md "Known limitation: SSRF" for details.
package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"

	"goravel/app/support/imageconfig"
)

var (
	ErrUnsupportedScheme = errors.New("image_url must be http or https")
	ErrTooManyRedirects  = errors.New("too many redirects")
	ErrResponseTooLarge  = errors.New("source image exceeds the configured maximum download size")
	ErrBadStatus         = errors.New("source URL returned a non-2xx status")
)

// Result is a downloaded source image, streamed to a temp file rather than
// held fully in memory.
type Result struct {
	// Path is the temporary file the image body was streamed to. The caller
	// owns cleanup (os.Remove) once done - see the job's defer.
	Path        string
	ContentType string
	Size        int64
}

// Download streams sourceURL to a temp file, enforcing connection/total
// timeouts, a maximum redirect count, and a maximum byte size. It never
// buffers the whole remote response in memory.
func Download(ctx context.Context, sourceURL string) (*Result, error) {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return nil, fmt.Errorf("parse image_url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrUnsupportedScheme
	}

	maxRedirects := imageconfig.MaxRedirects()
	client := &http.Client{
		Timeout: imageconfig.MaxSourceDownloadTime(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return ErrTooManyRedirects
			}
			return nil
		},
		Transport: &http.Transport{
			// Bounds only the TCP connect phase; the overall request is
			// still capped by client.Timeout above.
			DialContext: (&net.Dialer{Timeout: imageconfig.MaxConnectionTimeout()}).DialContext,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "image-resizer-goravel/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download source image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: got %d", ErrBadStatus, resp.StatusCode)
	}

	maxSize := imageconfig.MaxSourceDownloadSize()
	if resp.ContentLength > 0 && resp.ContentLength > maxSize {
		return nil, ErrResponseTooLarge
	}

	tmp, err := os.CreateTemp("", "image-resizer-src-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	defer tmp.Close()

	limited := io.LimitReader(resp.Body, maxSize+1)
	written, err := io.Copy(tmp, limited)
	if err != nil {
		os.Remove(tmp.Name())
		return nil, fmt.Errorf("write source image: %w", err)
	}
	if written > maxSize {
		os.Remove(tmp.Name())
		return nil, ErrResponseTooLarge
	}

	return &Result{
		Path:        tmp.Name(),
		ContentType: resp.Header.Get("Content-Type"),
		Size:        written,
	}, nil
}
