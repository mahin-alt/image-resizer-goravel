package bootstrap

import (
	"github.com/goravel/framework/contracts/foundation"
	"github.com/goravel/framework/contracts/queue"

	"goravel/app/facades"
	"goravel/app/support/imageconfig"
)

// queueRunner starts a Goravel queue worker scoped to one named queue with
// its own concurrency, as a framework Runner (started/stopped alongside the
// HTTP server by app.Start()/app.Shutdown()).
//
// Image processing itself no longer goes through a queue - POST /images and
// POST /images/{id}/retry run it synchronously, inline in the HTTP handler
// (see services.ProcessImageRequestSync) - so only CLEANUP_QUEUE's worker
// remains registered here. It (like the job it would run,
// CleanupExpiredImagesJob) is currently unused - see that job's own
// comment - but kept running for the same reason: in case
// retention-based cleanup is reintroduced later.
type queueRunner struct {
	name       string
	queueName  string
	concurrent int
	worker     queue.Worker
}

func newQueueRunner(name, queueName string, concurrent int) *queueRunner {
	return &queueRunner{name: name, queueName: queueName, concurrent: concurrent}
}

func (r *queueRunner) Signature() string { return "image-resizer:queue:" + r.name }
func (r *queueRunner) ShouldRun() bool   { return true }

func (r *queueRunner) Run() error {
	r.worker = facades.Queue().Worker(queue.Args{
		Queue:      r.queueName,
		Concurrent: r.concurrent,
	})
	return r.worker.Run()
}

func (r *queueRunner) Shutdown() error {
	if r.worker == nil {
		return nil
	}
	return r.worker.Shutdown()
}

// Runners registers the cleanup-queue worker described above.
func Runners() []foundation.Runner {
	return []foundation.Runner{
		newQueueRunner("cleanup", imageconfig.CleanupQueue(), 1),
	}
}
