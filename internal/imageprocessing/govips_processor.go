package imageprocessing

import (
	"context"
	"fmt"
	"sync"

	"github.com/davidbyttow/govips/v2/vips"

	"goravel/app/models"
)

var (
	startupOnce sync.Once
)

// Startup initializes libvips once for the whole process. Call it exactly
// once, from application bootstrap, before any job runs. concurrency sets
// libvips' internal worker-thread cap per operation (VIPS_CONCURRENCY) - see
// app/support/imageconfig.VipsConcurrency and the concurrency discussion in
// the README.
func Startup(concurrency int) {
	startupOnce.Do(func() {
		vips.LoggingSettings(nil, vips.LogLevelWarning)
		vips.Startup(&vips.Config{
			ConcurrencyLevel: concurrency,
		})
	})
}

// Shutdown releases libvips resources. Call it once, on graceful shutdown.
func Shutdown() {
	vips.Shutdown()
}

// GovipsImageProcessor is the only file in the application that imports
// govips/libvips directly.
type GovipsImageProcessor struct{}

func NewGovipsImageProcessor() *GovipsImageProcessor {
	return &GovipsImageProcessor{}
}

func (p *GovipsImageProcessor) Inspect(_ context.Context, sourcePath string) (SourceInfo, error) {
	// No NumPages/"n" here: that libvips loader option only exists on
	// multi-page formats (gif/webp/tiff/pdf) - setting it unconditionally
	// breaks single-page formats like JPEG ("no property named `n'"). It
	// isn't needed anyway: every multi-page loader already defaults "n" to
	// 1 (first frame only), which is exactly the animated-source policy we
	// want - see ProcessSizes below.
	img, err := vips.NewImageFromFile(sourcePath)
	if err != nil {
		return SourceInfo{}, fmt.Errorf("decode source image: %w", err)
	}
	defer img.Close()

	return SourceInfo{Width: img.Width(), Height: img.Height()}, nil
}

func (p *GovipsImageProcessor) ProcessSizes(_ context.Context, sourcePath string, sizes []SizeSpec) ([]Output, []SizeError, error) {
	// Decode once (autorotate; first-frame-only is already the default for
	// multi-page loaders - see the Inspect comment above), reused for every
	// requested size below - see the "decode once, resize many" rationale
	// in the architecture plan.
	params := vips.NewImportParams()
	params.AutoRotate.Set(true)

	// A throwaway load purely to validate the source decodes at all and to
	// obtain its native dimensions for upscale decisions.
	source, err := vips.LoadImageFromFile(sourcePath, params)
	if err != nil {
		return nil, nil, fmt.Errorf("decode source image: %w", err)
	}
	sourceWidth, sourceHeight := source.Width(), source.Height()
	source.Close()

	outputs := make([]Output, 0, len(sizes))
	var sizeErrs []SizeError

	for _, spec := range sizes {
		out, err := p.processOne(sourcePath, params, sourceWidth, sourceHeight, spec)
		if err != nil {
			sizeErrs = append(sizeErrs, SizeError{RequestSizeID: spec.RequestSizeID, Err: err})
			continue
		}
		outputs = append(outputs, out)
	}

	return outputs, sizeErrs, nil
}

func (p *GovipsImageProcessor) processOne(sourcePath string, params *vips.ImportParams, sourceWidth, sourceHeight int, spec SizeSpec) (Output, error) {
	width, height := spec.Width, spec.Height

	crop := vips.InterestingNone
	size := vips.SizeBoth
	if !spec.AllowUpscale {
		size = vips.SizeDown
	}

	switch spec.Mode {
	case models.ResizeModeContain, models.ResizeModeFit:
		crop = vips.InterestingNone
	case models.ResizeModeCover:
		crop = vips.InterestingAttention
	case models.ResizeModeCrop:
		crop = vips.InterestingCentre
	case models.ResizeModeFill:
		crop = vips.InterestingNone
		size = vips.SizeForce
		if !spec.AllowUpscale {
			// SizeForce always hits the exact box, which would upscale.
			// Cap the target at the source's own dimensions per axis so a
			// disabled-upscale request never enlarges beyond the source,
			// while still stretching to the (possibly smaller) requested
			// aspect ratio.
			if width > sourceWidth {
				width = sourceWidth
			}
			if height > sourceHeight {
				height = sourceHeight
			}
		}
	default:
		return Output{}, fmt.Errorf("unsupported resize mode %q", spec.Mode)
	}

	img, err := vips.LoadThumbnailFromFile(sourcePath, width, height, crop, size, params)
	if err != nil {
		return Output{}, fmt.Errorf("resize to %dx%d (%s): %w", spec.Width, spec.Height, spec.Mode, err)
	}
	defer img.Close()

	buf, _, err := img.ExportWebp(&vips.WebpExportParams{
		Quality:       spec.Quality,
		StripMetadata: true,
	})
	if err != nil {
		return Output{}, fmt.Errorf("encode webp: %w", err)
	}

	return Output{
		RequestSizeID: spec.RequestSizeID,
		Width:         img.Width(),
		Height:        img.Height(),
		Mode:          spec.Mode,
		Bytes:         buf,
	}, nil
}
