package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"goravel/app/facades"
)

// Adds support for local file uploads as a second way to supply the source
// image, alongside image_url. input_type records which one a given request
// used ("url" or "upload"); source_file_path holds the storage path of an
// uploaded original (nil for url-based requests).
type M20260902000001AddInputTypeToImageProcessingRequestsTable struct{}

func (r *M20260902000001AddInputTypeToImageProcessingRequestsTable) Signature() string {
	return "20260902000001_add_input_type_to_image_processing_requests_table"
}

func (r *M20260902000001AddInputTypeToImageProcessingRequestsTable) Up() error {
	if !facades.Schema().HasColumn("image_processing_requests", "input_type") {
		if err := facades.Schema().Table("image_processing_requests", func(table schema.Blueprint) {
			table.String("input_type", 16).Default("url")
			table.Text("source_file_path").Nullable()
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *M20260902000001AddInputTypeToImageProcessingRequestsTable) Down() error {
	return facades.Schema().Table("image_processing_requests", func(table schema.Blueprint) {
		table.DropColumn("input_type", "source_file_path")
	})
}
