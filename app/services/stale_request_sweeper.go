package services

import (
	"goravel/app/facades"
	"goravel/app/models"
	"goravel/app/support/imageconfig"

	"github.com/goravel/framework/support/carbon"
)

const staleProcessingErrorMessage = "Processing was interrupted and did not finish in time (the worker handling it likely crashed or was shut down). You can retry this request."

// SweepStaleProcessingRequests is run on a schedule (see bootstrap/schedule.go)
// to catch requests whose worker died mid-job - a crash or a forced
// shutdown leaves ProcessImageRequestJob's goroutine gone without ever
// writing a terminal status, so nothing else notices. A request is
// considered abandoned once it's been "processing" without a fresh
// last_heartbeat_at (see the job's startHeartbeat) for longer than
// imageconfig.StaleProcessingTimeout. Falls back to started_at for the rare
// case a request reached "processing" but the process died before its
// first heartbeat write ever landed.
func SweepStaleProcessingRequests() {
	threshold := carbon.Now().SubSeconds(int(imageconfig.StaleProcessingTimeout().Seconds()))

	result, err := facades.Orm().Query().Model(&models.ImageProcessingRequest{}).
		Where("status = ?", models.RequestStatusProcessing).
		Where("(last_heartbeat_at IS NOT NULL AND last_heartbeat_at < ?) OR (last_heartbeat_at IS NULL AND started_at < ?)", threshold, threshold).
		Update(map[string]any{
			"status":        models.RequestStatusFailed,
			"error_message": staleProcessingErrorMessage,
			"completed_at":  carbon.NewDateTime(carbon.Now()),
		})
	if err != nil {
		facades.Log().With(map[string]any{"error": err.Error()}).Error("stale processing sweep failed")
		return
	}
	if result != nil && result.RowsAffected > 0 {
		facades.Log().With(map[string]any{"count": result.RowsAffected}).
			Warning("marked stale processing request(s) as failed")
	}
}
