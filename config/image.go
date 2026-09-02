package config

import (
	"goravel/app/facades"
)

// This file centralizes every environment-driven setting used by the image
// processing pipeline. No other package should call config.Env()/os.Getenv()
// for these values directly - read them from facades.Config().Get("image....")
// (see app/support/imageconfig for a typed accessor).
func init() {
	config := facades.Config()
	config.Add("image", map[string]any{
		// WebP encoding.
		//
		// DefaultQuality is used when a request doesn't specify a per-size
		// quality override. QualityMin/QualityMax clamp any client-supplied
		// override so a request can't demand a pathologically large/slow
		// encode or a visibly broken one.
		"default_quality": config.Env("IMAGE_DEFAULT_QUALITY", 80),
		// 0-100 is libvips' full supported range for the WebP encoder's Q
		// factor (confirmed via `vips webpsave`: "min: 0, max: 100") - these
		// bounds are the library's actual floor/ceiling, not an arbitrary
		// choice.
		"quality_min": config.Env("IMAGE_QUALITY_MIN", 0),
		"quality_max": config.Env("IMAGE_QUALITY_MAX", 100),

		// How long a generated output stays available after creation, and
		// therefore how expires_at is computed at creation time (never
		// recomputed later from this value).
		"retention_seconds": config.Env("IMAGE_RETENTION_SECONDS", 86400),

		// Whether outputs may be enlarged beyond the source image's
		// dimensions by default. A request can override this per size.
		"allow_upscale": config.Env("ALLOW_UPSCALE", false),

		// Resource limits. All are enforced before/while processing.
		// max_image_file_size also caps uploaded file size (see the
		// controller).
		"max_image_file_size":     config.Env("MAX_IMAGE_FILE_SIZE", int64(25*1024*1024)),
		"max_image_width":         config.Env("MAX_IMAGE_WIDTH", 8000),
		"max_image_height":        config.Env("MAX_IMAGE_HEIGHT", 8000),
		"max_total_output_pixels": config.Env("MAX_TOTAL_OUTPUT_PIXELS", 64_000_000),
		"max_sizes_per_request":   config.Env("MAX_SIZES_PER_REQUEST", 10),

		// Concurrency. MaxConcurrentImageJobs bounds how many
		// ProcessImageRequestJob run at once (queue worker concurrency for
		// PROCESSING_QUEUE); VipsConcurrency bounds how many threads libvips
		// itself uses per operation inside a single job. Keep the product of
		// the two roughly at or below the number of CPU cores available to
		// the worker process - see docs/README for tuning guidance.
		"max_concurrent_image_jobs": config.Env("MAX_CONCURRENT_IMAGE_JOBS", 4),
		"vips_concurrency":          config.Env("VIPS_CONCURRENCY", 2),

		// Storage.
		"storage_path": config.Env("STORAGE_PATH", "storage/app/images"),

		// Queues.
		"processing_queue": config.Env("PROCESSING_QUEUE", "image_processing"),
		"cleanup_queue":    config.Env("CLEANUP_QUEUE", "image_cleanup"),
	})
}
