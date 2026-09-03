package bootstrap

import (
	"github.com/goravel/framework/contracts/database/schema"

	"goravel/database/migrations"
)

func Migrations() []schema.Migration {
	return []schema.Migration{
		&migrations.M20210101000001CreateJobsTable{},
		&migrations.M20260901000001CreateImageProcessingRequestsTable{},
		&migrations.M20260901000002CreateImageProcessingRequestSizesTable{},
		&migrations.M20260901000003CreateImageOutputsTable{},
		&migrations.M20260901000004AddSourceDimensionsToImageProcessingRequestsTable{},
		&migrations.M20260902000001AddInputTypeToImageProcessingRequestsTable{},
		&migrations.M20260902000002MakeExpiresAtNullableOnImageOutputsTable{},
	}
}
