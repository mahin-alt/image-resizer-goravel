package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"goravel/app/facades"
)

// Adds the source image's own dimensions to image_processing_requests, so
// GET /api/v1/images/{id} can report them alongside the generated outputs.
// They're nullable because they aren't known until the worker has
// downloaded and inspected the source (status pending/processing).
type M20260901000004AddSourceDimensionsToImageProcessingRequestsTable struct{}

func (r *M20260901000004AddSourceDimensionsToImageProcessingRequestsTable) Signature() string {
	return "20260901000004_add_source_dimensions_to_image_processing_requests_table"
}

func (r *M20260901000004AddSourceDimensionsToImageProcessingRequestsTable) Up() error {
	if !facades.Schema().HasColumn("image_processing_requests", "source_width") {
		if err := facades.Schema().Table("image_processing_requests", func(table schema.Blueprint) {
			table.Integer("source_width").Nullable()
			table.Integer("source_height").Nullable()
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *M20260901000004AddSourceDimensionsToImageProcessingRequestsTable) Down() error {
	return facades.Schema().Table("image_processing_requests", func(table schema.Blueprint) {
		table.DropColumn("source_width", "source_height")
	})
}
