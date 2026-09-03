package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"goravel/app/facades"
)

// output_hash is the single random name shared by every generated output of
// one request (see app/storage.NewOutputHash and
// ProcessImageRequestJob.outputHashFor) - outputs are stored as
// media/{width}x{height}/{output_hash}.webp, so all sizes of the same
// source image resolve to the same filename across their size folders.
// It's generated once (lazily, on first use) and persisted here so a job
// retry reuses the same hash instead of orphaning previously-written sizes
// under a different name.
type M20260903000001AddOutputHashToImageProcessingRequestsTable struct{}

func (r *M20260903000001AddOutputHashToImageProcessingRequestsTable) Signature() string {
	return "20260903000001_add_output_hash_to_image_processing_requests_table"
}

func (r *M20260903000001AddOutputHashToImageProcessingRequestsTable) Up() error {
	if !facades.Schema().HasColumn("image_processing_requests", "output_hash") {
		if err := facades.Schema().Table("image_processing_requests", func(table schema.Blueprint) {
			table.String("output_hash", 64).Nullable()
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *M20260903000001AddOutputHashToImageProcessingRequestsTable) Down() error {
	return facades.Schema().Table("image_processing_requests", func(table schema.Blueprint) {
		table.DropColumn("output_hash")
	})
}
