// Package services holds application-level orchestration that controllers
// call into. Keeping it here (rather than in the controller) is what lets
// controllers stay thin per the architecture plan.
package services

import (
	"errors"
	"fmt"

	"github.com/goravel/framework/contracts/database/orm"
	"github.com/goravel/framework/contracts/filesystem"
	"github.com/goravel/framework/contracts/queue"

	"goravel/app/facades"
	"goravel/app/http/requests"
	"goravel/app/jobs"
	"goravel/app/models"
	"goravel/app/storage"
	"goravel/app/support/imageconfig"
)

var (
	// ErrRequestNotFound is returned by RetryImageProcessingRequest when the
	// given id doesn't exist.
	ErrRequestNotFound = errors.New("image processing request not found")
	// ErrRequestNotRetryable is returned by RetryImageProcessingRequest for
	// a request whose status isn't one that indicates something actually
	// failed (only "failed" and "partially_completed" qualify - see the
	// status constants in app/models).
	ErrRequestNotRetryable = errors.New("only failed or partially completed requests can be retried")
)

// CreateImageProcessingRequest persists a pending ImageProcessingRequest
// plus one ImageProcessingRequestSize per requested size, stores the
// uploaded file onto the "images" disk under uploads/{request_id}/ (never
// using the client-supplied filename), then dispatches the single
// processing job for it (see the "one job per request" decision in the
// architecture plan). It does not touch govips - that all happens in the
// worker, which reads the file directly from where it's stored here and
// deletes it once processing finishes.
func CreateImageProcessingRequest(file filesystem.File, sizes []requests.RequestedSize) (*models.ImageProcessingRequest, error) {
	request := &models.ImageProcessingRequest{
		InputType: models.InputTypeUpload,
		Status:    models.RequestStatusPending,
	}

	err := facades.Orm().Transaction(func(tx orm.Query) error {
		if err := tx.Create(request); err != nil {
			return fmt.Errorf("create processing request: %w", err)
		}

		for _, s := range sizes {
			mode := s.Mode
			if mode == "" {
				mode = models.ResizeModeContain
			}

			size := &models.ImageProcessingRequestSize{
				ImageProcessingRequestID: request.ID,
				Width:                    s.Width,
				Height:                   s.Height,
				Mode:                     mode,
				AllowUpscale:             imageconfig.AllowUpscaleDefault(),
				Quality:                  imageconfig.ClampQuality(s.Quality),
				Status:                   models.SizeStatusPending,
			}
			if err := tx.Create(size); err != nil {
				return fmt.Errorf("create requested size: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	storedPath, err := storage.PutUploadedFile(storage.UploadDir(request.ID), file)
	if err != nil {
		return nil, fmt.Errorf("store uploaded file: %w", err)
	}

	if _, err := facades.Orm().Query().Model(&models.ImageProcessingRequest{}).
		Where("id", request.ID).
		Update("source_file_path", storedPath); err != nil {
		return nil, fmt.Errorf("record uploaded file path: %w", err)
	}
	request.SourceFilePath = &storedPath

	if err := facades.Queue().
		Job(&jobs.ProcessImageRequestJob{}, []queue.Arg{{Type: "uint", Value: request.ID}}).
		OnQueue(imageconfig.ProcessingQueue()).
		Dispatch(); err != nil {
		return nil, fmt.Errorf("dispatch processing job: %w", err)
	}

	return request, nil
}

// RetryImageProcessingRequest resets a "failed" or "partially_completed"
// request back to "pending" and re-dispatches ProcessImageRequestJob for
// it. Only those two statuses are accepted (see ErrRequestNotRetryable) -
// they're the ones where something actually failed; a "completed" request
// has nothing to retry, and a "pending"/"processing" one is either already
// queued or already being worked (or, if genuinely stuck, is
// services.SweepStaleProcessingRequests's job to first flip to "failed"
// before this becomes usable on it).
//
// Note: the job deletes the original uploaded source file once it finishes
// (success or failure) as part of normal cleanup - see
// ProcessImageRequestJob.resolveSource. A request that failed via that
// normal path (as opposed to being caught mid-crash by the stale sweep)
// will retry into the same "no stored source file" failure, since there's
// no source left to reprocess. That failure is still surfaced clearly via
// error_message rather than silently no-op'ing.
func RetryImageProcessingRequest(id uint) (*models.ImageProcessingRequest, error) {
	var request models.ImageProcessingRequest
	if err := facades.Orm().Query().Where("id", id).First(&request); err != nil {
		return nil, fmt.Errorf("load request %d: %w", id, err)
	}
	if request.ID == 0 {
		return nil, ErrRequestNotFound
	}

	switch request.Status {
	case models.RequestStatusFailed, models.RequestStatusPartiallyCompleted:
		// retryable
	default:
		return nil, ErrRequestNotRetryable
	}

	err := facades.Orm().Transaction(func(tx orm.Query) error {
		if _, err := tx.Model(&models.ImageProcessingRequest{}).Where("id", request.ID).
			Update(map[string]any{
				"status":            models.RequestStatusPending,
				"error_message":     nil,
				"started_at":        nil,
				"completed_at":      nil,
				"last_heartbeat_at": nil,
			}); err != nil {
			return fmt.Errorf("reset request: %w", err)
		}

		if _, err := tx.Model(&models.ImageProcessingRequestSize{}).
			Where("image_processing_request_id", request.ID).
			Update(map[string]any{"status": models.SizeStatusPending, "error_message": nil}); err != nil {
			return fmt.Errorf("reset requested sizes: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	request.Status = models.RequestStatusPending

	if err := facades.Queue().
		Job(&jobs.ProcessImageRequestJob{}, []queue.Arg{{Type: "uint", Value: request.ID}}).
		OnQueue(imageconfig.ProcessingQueue()).
		Dispatch(); err != nil {
		return nil, fmt.Errorf("dispatch retry job: %w", err)
	}

	return &request, nil
}
