// Package imageprocessing defines the application-level image processing
// abstraction. Nothing outside this package (and its govips adapter file)
// should import govips directly - controllers, jobs and services depend only
// on the ImageProcessor interface and the plain types below.
package imageprocessing

import "context"

// MaxSupportedDimension is libvips' own hard sanity ceiling for a single
// image coordinate (width or height) - VIPS_DEFAULT_MAX_COORD in
// vips/image.h ("We have a maximum value for a coordinate at various points
// for sanity checking... it's good to have a lower value set so we can see
// crazy numbers early"). No width/height beyond this can ever be produced
// by this library, regardless of any app-level config, so requested output
// sizes are validated against it independently of the configurable
// MAX_IMAGE_WIDTH/MAX_IMAGE_HEIGHT business limits (see imageconfig).
const MaxSupportedDimension = 100_000_000

// SizeSpec is one requested output: dimensions, resize mode, and the
// already-resolved (clamped/defaulted) upscale and quality settings.
type SizeSpec struct {
	// RequestSizeID correlates a SizeSpec back to its
	// models.ImageProcessingRequestSize row so the caller can update/attach
	// results without guessing by position.
	RequestSizeID uint
	Width         int
	Height        int
	Mode          string // one of the models.ResizeMode* constants
	AllowUpscale  bool
	Quality       int // already clamped to [QualityMin, QualityMax]
}

// Output is one successfully generated image, still in memory as WebP bytes.
// The caller (the job) is responsible for persisting it via the storage
// abstraction and recording an ImageOutput row.
type Output struct {
	RequestSizeID uint
	Width         int
	Height        int
	Mode          string
	Bytes         []byte
}

// SizeError records a per-size failure without aborting the rest of the
// request (see the "partially_completed" behavior in the plan/README).
type SizeError struct {
	RequestSizeID uint
	Err           error
}

func (e *SizeError) Error() string { return e.Err.Error() }
func (e *SizeError) Unwrap() error { return e.Err }

// SourceInfo describes the decoded source image, used for validation
// (dimension/pixel limits) before any resizing is attempted.
type SourceInfo struct {
	Width  int
	Height int
}

// ImageProcessor is the application's image-processing abstraction. The only
// concrete implementation is GovipsImageProcessor (govips_processor.go).
type ImageProcessor interface {
	// Inspect opens sourcePath just far enough to report its dimensions,
	// without decoding pixel data, so callers can reject oversized/malformed
	// images before doing real work.
	Inspect(ctx context.Context, sourcePath string) (SourceInfo, error)

	// ProcessSizes decodes sourcePath once and produces one WebP Output per
	// requested SizeSpec. A failure on an individual size is returned inside
	// the result (via Errors), not as the function's error return; the
	// function's error return is reserved for failures that make the whole
	// source unusable (corrupt file, unsupported format, decode failure).
	ProcessSizes(ctx context.Context, sourcePath string, sizes []SizeSpec) ([]Output, []SizeError, error)
}
