package jobs

import (
	"github.com/goravel/framework/support/carbon"

	"goravel/app/facades"
	"goravel/app/models"
	"goravel/app/storage"
)

// CleanupExpiredImagesJob deletes ImageOutput files/rows whose ExpiresAt has
// passed. It is dispatched on a schedule (bootstrap/app.go) onto the
// dedicated cleanup queue so a backlog of ProcessImageRequestJob work can
// never starve it.
//
// It is intentionally idempotent and defensive: a missing file, a
// double-delete, an already-gone row, or a crash mid-run must all be safe to
// hit again on the next scheduled run - nothing here assumes the process
// stays alive continuously or that this is the only time it will run over a
// given row.
type CleanupExpiredImagesJob struct{}

func (r *CleanupExpiredImagesJob) Signature() string {
	return "cleanup_expired_images"
}

func (r *CleanupExpiredImagesJob) Handle(args ...any) error {
	var expired []models.ImageOutput
	if err := facades.Orm().Query().
		Where("expires_at <= ?", carbon.NewDateTime(carbon.Now())).
		Find(&expired); err != nil {
		return err
	}

	for _, output := range expired {
		// Best-effort delete: a missing file is not an error, it just means
		// a previous run (or a manual cleanup) already removed it.
		if output.StoragePath != "" && storage.Exists(output.StoragePath) {
			if err := storage.Delete(output.StoragePath); err != nil {
				facades.Log().With(map[string]any{
					"output_id": output.ID,
					"path":      output.StoragePath,
					"error":     err.Error(),
				}).Error("cleanup: failed to delete expired image file")
				continue // leave the row for the next run to retry
			}
		}

		o := output
		if _, err := facades.Orm().Query().Delete(&o); err != nil {
			facades.Log().With(map[string]any{
				"output_id": output.ID,
				"error":     err.Error(),
			}).Error("cleanup: failed to delete expired image row")
		}
	}

	facades.Log().With(map[string]any{"count": len(expired)}).Info("cleanup: expired images processed")
	return nil
}
