// Package imageconfig is the single place that reads the "image" config tree
// (config/image.go, itself populated from environment variables). Every other
// package (controllers, services, jobs, the govips adapter, the downloader)
// must go through these accessors instead of reading facades.Config() or
// os.Getenv() directly, so every limit stays defined in exactly one place.
package imageconfig

import (
	"strconv"
	"time"

	"goravel/app/facades"
)

func DefaultQuality() int { return facades.Config().GetInt("image.default_quality", 80) }
func QualityMin() int     { return facades.Config().GetInt("image.quality_min", 40) }
func QualityMax() int     { return facades.Config().GetInt("image.quality_max", 95) }

// ClampQuality resolves a possibly-nil client-supplied quality against the
// configured default and [QualityMin, QualityMax] bounds.
func ClampQuality(requested *int) int {
	q := DefaultQuality()
	if requested != nil {
		q = *requested
	}
	if min := QualityMin(); q < min {
		q = min
	}
	if max := QualityMax(); q > max {
		q = max
	}
	return q
}

func RetentionHours() int { return facades.Config().GetInt("image.retention_hours", 24) }

func RetentionDuration() time.Duration {
	return time.Duration(RetentionHours()) * time.Hour
}

func AllowUpscaleDefault() bool { return facades.Config().GetBool("image.allow_upscale", false) }

// ResolveUpscale resolves a possibly-nil per-size override against the global default.
func ResolveUpscale(override *bool) bool {
	if override != nil {
		return *override
	}
	return AllowUpscaleDefault()
}

func MaxImageFileSize() int64 {
	v := facades.Config().Get("image.max_image_file_size", int64(25*1024*1024))
	return toInt64(v)
}

func MaxImageWidth() int  { return facades.Config().GetInt("image.max_image_width", 8000) }
func MaxImageHeight() int { return facades.Config().GetInt("image.max_image_height", 8000) }

func MaxTotalOutputPixels() int64 {
	return toInt64(facades.Config().Get("image.max_total_output_pixels", 64_000_000))
}

func MaxSizesPerRequest() int {
	return facades.Config().GetInt("image.max_sizes_per_request", 10)
}

func MaxSourceDownloadSize() int64 {
	return toInt64(facades.Config().Get("image.max_source_download_size", int64(25*1024*1024)))
}

func MaxSourceDownloadTime() time.Duration {
	return facades.Config().GetDuration("image.max_source_download_time", 30*time.Second)
}

func MaxConnectionTimeout() time.Duration {
	return facades.Config().GetDuration("image.max_connection_timeout", 5*time.Second)
}

func MaxRedirects() int { return facades.Config().GetInt("image.max_redirects", 3) }

func MaxConcurrentImageJobs() int {
	return facades.Config().GetInt("image.max_concurrent_image_jobs", 4)
}

func VipsConcurrency() int { return facades.Config().GetInt("image.vips_concurrency", 2) }

func StoragePath() string {
	return facades.Config().GetString("image.storage_path", "storage/app/images")
}

func ProcessingQueue() string {
	return facades.Config().GetString("image.processing_queue", "image_processing")
}

func CleanupQueue() string {
	return facades.Config().GetString("image.cleanup_queue", "image_cleanup")
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		// config.Env() returns the raw string when the env var is actually
		// set (it does not cast to the type of the default value passed to
		// config.Add) - only the framework's typed Get*() accessors
		// (GetInt/GetBool/GetDuration) do that casting for us. Handle it
		// here too so a value like MAX_SOURCE_DOWNLOAD_SIZE=26214400 in
		// .env isn't silently read as 0.
		parsed, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}
