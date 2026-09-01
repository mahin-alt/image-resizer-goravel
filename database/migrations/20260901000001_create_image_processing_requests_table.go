package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"goravel/app/facades"
)

type M20260901000001CreateImageProcessingRequestsTable struct{}

func (r *M20260901000001CreateImageProcessingRequestsTable) Signature() string {
	return "20260901000001_create_image_processing_requests_table"
}

func (r *M20260901000001CreateImageProcessingRequestsTable) Up() error {
	if facades.Schema().HasTable("image_processing_requests") {
		return nil
	}

	return facades.Schema().Create("image_processing_requests", func(table schema.Blueprint) {
		table.ID()
		table.Text("source_url")
		table.String("status", 32).Default("pending")
		table.Text("error_message").Nullable()
		table.DateTimeTz("started_at").Nullable()
		table.DateTimeTz("completed_at").Nullable()
		table.DateTimeTz("created_at").UseCurrent()
		table.DateTimeTz("updated_at").UseCurrent()
		table.Index("status")
	})
}

func (r *M20260901000001CreateImageProcessingRequestsTable) Down() error {
	return facades.Schema().DropIfExists("image_processing_requests")
}
