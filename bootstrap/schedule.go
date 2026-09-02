package bootstrap

import (
	"github.com/goravel/framework/contracts/schedule"

	"goravel/app/facades"
	"goravel/app/jobs"
	"goravel/app/support/imageconfig"
)

// Schedule dispatches CleanupExpiredImagesJob onto the dedicated cleanup
// queue every minute. OnOneServer keeps a multi-worker deployment from
// double-dispatching it every tick.
//
// This runs every minute (rather than hourly) because IMAGE_RETENTION_SECONDS
// is second-granular - a retention of e.g. 30s would be pointless if cleanup
// only checked once an hour. A minute is the practical floor: an output can
// therefore live up to ~1 minute past its configured retention before it's
// actually deleted. If you need tighter-than-a-minute cleanup latency,
// change EveryMinute() below to e.g. EveryTenSeconds(), at the cost of more
// frequent DB polling.
func Schedule() []schedule.Event {
	return []schedule.Event{
		facades.Schedule().Call(func() {
			_ = facades.Queue().
				Job(&jobs.CleanupExpiredImagesJob{}).
				OnQueue(imageconfig.CleanupQueue()).
				Dispatch()
		}).Name("dispatch-cleanup-expired-images").EveryMinute().OnOneServer(),
	}
}
