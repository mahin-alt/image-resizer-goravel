package bootstrap

import (
	"github.com/goravel/framework/contracts/schedule"

	"goravel/app/facades"
	"goravel/app/services"
)

// Schedule runs services.SweepStaleProcessingRequests every minute - it
// catches requests left stuck at "processing" by a worker that crashed or
// was shut down mid-job and marks them failed (retryable) instead of
// leaving them stuck forever - see the "stale request recovery" plan
// section.
//
// CleanupExpiredImagesJob (app/jobs/cleanup_expired_images_job.go) is not
// scheduled: generated outputs are permanent now, so there's nothing for it
// to do. It and its registration in bootstrap/jobs.go are left in place
// unused, in case retention-based cleanup is reintroduced later.
func Schedule() []schedule.Event {
	return []schedule.Event{
		facades.Schedule().Call(services.SweepStaleProcessingRequests).EveryMinute(),
	}
}
