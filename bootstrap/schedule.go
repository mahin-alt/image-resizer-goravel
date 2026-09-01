package bootstrap

import (
	"github.com/goravel/framework/contracts/schedule"

	"goravel/app/facades"
	"goravel/app/jobs"
	"goravel/app/support/imageconfig"
)

// Schedule dispatches CleanupExpiredImagesJob onto the dedicated cleanup
// queue once an hour. OnOneServer keeps a multi-worker deployment from
// double-dispatching it every tick.
func Schedule() []schedule.Event {
	return []schedule.Event{
		facades.Schedule().Call(func() {
			_ = facades.Queue().
				Job(&jobs.CleanupExpiredImagesJob{}).
				OnQueue(imageconfig.CleanupQueue()).
				Dispatch()
		}).Name("dispatch-cleanup-expired-images").Hourly().OnOneServer(),
	}
}
