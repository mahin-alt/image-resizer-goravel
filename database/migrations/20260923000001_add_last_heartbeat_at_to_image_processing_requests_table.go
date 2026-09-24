package migrations

import (
	"github.com/goravel/framework/contracts/database/schema"

	"goravel/app/facades"
)

// last_heartbeat_at is updated periodically by ProcessImageRequestJob while
// it's actively working a request (see the job's startHeartbeat), and read
// by services.SweepStaleProcessingRequests to tell a request whose worker
// died mid-job (crash, forced shutdown) apart from one that's just slow -
// see the "stale request recovery" plan section.
type M20260923000001AddLastHeartbeatAtToImageProcessingRequestsTable struct{}

func (r *M20260923000001AddLastHeartbeatAtToImageProcessingRequestsTable) Signature() string {
	return "20260923000001_add_last_heartbeat_at_to_image_processing_requests_table"
}

func (r *M20260923000001AddLastHeartbeatAtToImageProcessingRequestsTable) Up() error {
	if facades.Schema().HasColumn("image_processing_requests", "last_heartbeat_at") {
		return nil
	}
	return facades.Schema().Table("image_processing_requests", func(table schema.Blueprint) {
		table.DateTimeTz("last_heartbeat_at").Nullable()
	})
}

func (r *M20260923000001AddLastHeartbeatAtToImageProcessingRequestsTable) Down() error {
	return facades.Schema().Table("image_processing_requests", func(table schema.Blueprint) {
		table.DropColumn("last_heartbeat_at")
	})
}
