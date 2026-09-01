package bootstrap

import (
	"github.com/goravel/framework/contracts/foundation"
	"github.com/goravel/framework/contracts/queue"

	"goravel/app/facades"
	"goravel/app/support/imageconfig"
)

// queueRunner starts a Goravel queue worker scoped to one named queue with
// its own concurrency, as a framework Runner (started/stopped alongside the
// HTTP server by app.Start()/app.Shutdown()). We use two of these - one for
// PROCESSING_QUEUE, one for CLEANUP_QUEUE - rather than the framework's
// single default-connection worker, precisely so a backlog of heavy
// ProcessImageRequestJob work can never starve cleanup (see the plan's
// "Queue architecture" section).
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

// Runners registers the two named-queue workers described above.
func Runners() []foundation.Runner {
	return []foundation.Runner{
		newQueueRunner("processing", imageconfig.ProcessingQueue(), imageconfig.MaxConcurrentImageJobs()),
		newQueueRunner("cleanup", imageconfig.CleanupQueue(), 1),
	}
}
