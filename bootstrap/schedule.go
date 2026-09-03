package bootstrap

import (
	"github.com/goravel/framework/contracts/schedule"
)

// Schedule is currently empty: generated outputs are permanent, so
// CleanupExpiredImagesJob (app/jobs/cleanup_expired_images_job.go) is no
// longer dispatched. The job and its registration in bootstrap/jobs.go are
// left in place unused, in case retention-based cleanup is reintroduced
// later.
func Schedule() []schedule.Event {
	return []schedule.Event{}
}
