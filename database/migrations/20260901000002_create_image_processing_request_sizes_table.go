package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"goravel/app/facades"
)

type M20260901000002CreateImageProcessingRequestSizesTable struct{}

func (r *M20260901000002CreateImageProcessingRequestSizesTable) Signature() string {
	return "20260901000002_create_image_processing_request_sizes_table"
}

func (r *M20260901000002CreateImageProcessingRequestSizesTable) Up() error {
	if facades.Schema().HasTable("image_processing_request_sizes") {
		return nil
	}

	return facades.Schema().Create("image_processing_request_sizes", func(table schema.Blueprint) {
		table.ID()
		table.UnsignedBigInteger("image_processing_request_id")
		table.Integer("width")
		table.Integer("height")
		table.String("mode", 16)
		table.Boolean("allow_upscale").Default(false)
		table.Integer("quality")
		table.String("status", 16).Default("pending")
		table.Text("error_message").Nullable()
		table.DateTimeTz("created_at").UseCurrent()
		table.DateTimeTz("updated_at").UseCurrent()
		table.Index("image_processing_request_id")
	})
}

func (r *M20260901000002CreateImageProcessingRequestSizesTable) Down() error {
	return facades.Schema().DropIfExists("image_processing_request_sizes")
}
