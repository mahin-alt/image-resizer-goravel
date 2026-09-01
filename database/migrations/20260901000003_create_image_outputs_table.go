package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"goravel/app/facades"
)

type M20260901000003CreateImageOutputsTable struct{}

func (r *M20260901000003CreateImageOutputsTable) Signature() string {
	return "20260901000003_create_image_outputs_table"
}

func (r *M20260901000003CreateImageOutputsTable) Up() error {
	if facades.Schema().HasTable("image_outputs") {
		return nil
	}

	return facades.Schema().Create("image_outputs", func(table schema.Blueprint) {
		table.ID()
		table.UnsignedBigInteger("image_processing_request_id")
		table.UnsignedBigInteger("image_processing_request_size_id")
		table.Integer("width")
		table.Integer("height")
		table.String("mode", 16)
		table.String("format", 8).Default("webp")
		table.Text("storage_path")
		table.BigInteger("file_size")
		table.DateTimeTz("expires_at")
		table.DateTimeTz("created_at").UseCurrent()
		table.DateTimeTz("updated_at").UseCurrent()
		table.Index("image_processing_request_id")
		table.Unique("image_processing_request_size_id")
		table.Index("expires_at")
	})
}

func (r *M20260901000003CreateImageOutputsTable) Down() error {
	return facades.Schema().DropIfExists("image_outputs")
}
