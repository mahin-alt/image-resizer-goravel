package bootstrap

import (
	contractsfoundation "github.com/goravel/framework/contracts/foundation"
	"github.com/goravel/framework/foundation"

	"goravel/config"
	"goravel/internal/imageprocessing"
	"goravel/routes"

	"goravel/app/support/imageconfig"
)

func Boot() contractsfoundation.Application {
	return foundation.Setup().
		WithMigrations(Migrations).
		WithRouting(func() {
			routes.Web()
			routes.Grpc()
			routes.Api()
		}).
		WithProviders(Providers).
		WithConfig(config.Boot).
		WithJobs(Jobs).
		WithSchedule(Schedule).
		WithRunners(Runners).
		WithCallback(func() {
			// libvips is initialized once for the whole process, here,
			// rather than per-job - see internal/imageprocessing.Startup.
			imageprocessing.Startup(imageconfig.VipsConcurrency())
		}).
		Create()
}
