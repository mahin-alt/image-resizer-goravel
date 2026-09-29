// Package services holds application-level orchestration that controllers
// call into. Keeping it here (rather than in the controller) is what lets
// controllers stay thin per the architecture plan.
package services

import (
	"errors"
	"fmt"

	"github.com/goravel/framework/contracts/database/orm"
	"github.com/goravel/framework/contracts/filesystem"

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

// processingSemaphore bounds how many requests this process runs through
// ProcessImageRequestSync at once. Processing now happens inline in the
// HTTP handler instead of a queue worker pool, so nothing else limits how
// much concurrent libvips work a burst of uploads could otherwise spawn -
// this is that limit, reusing the same MAX_CONCURRENT_IMAGE_JOBS setting
// that used to size the queue worker's own concurrency.
var processingSemaphore = make(chan struct{}, imageconfig.MaxConcurrentImageJobs())

// ProcessImageRequestSync runs ProcessImageRequestJob.Handle for requestID
// synchronously (rather than dispatching it to a queue), blocking the
// caller until it finishes. A returned error means processing itself
// failed for a reason worth logging (see ProcessImageRequestJob.Handle) -
// it does NOT mean the caller should treat the HTTP request as failed: the
// outcome (including "failed"/"partially_completed") is already durably
// recorded on the request row, which is what callers should read back and
// return to the client regardless of this return value.
func ProcessImageRequestSync(requestID uint) error {
	processingSemaphore <- struct{}{}
	defer func() { <-processingSemaphore }()

	return (&jobs.ProcessImageRequestJob{}).Handle(requestID)
}

// CreateImageProcessingRequest persists a pending ImageProcessingRequest
// plus one ImageProcessingRequestSize per requested size, stores the
// uploaded file onto the "images" disk under uploads/{request_id}/ (never
// using the client-supplied filename), then processes it synchronously
// before returning - the caller re-reads the request's final state (see
// controllers.loadRequestDetail) rather than trusting anything on the
// struct returned here, since this function's own copy is never updated
// past the initial insert.
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

	if err := ProcessImageRequestSync(request.ID); err != nil {
		// Already recorded on the request/size rows themselves (see
		// ProcessImageRequestJob.fail and markSizeFailed) - the caller
		// reads that back rather than treating this as an HTTP-level
		// failure. Logged here purely for operator visibility.
		facades.Log().With(map[string]any{"request_id": request.ID, "error": err.Error()}).
			Warning("image processing finished with an error")
	}

	return request, nil
}

// RetryImageProcessingRequest resets a "failed" or "partially_completed"
// request back to "pending" and reprocesses it synchronously, the same way
// CreateImageProcessingRequest does. Only those two statuses are accepted
// (see ErrRequestNotRetryable) -
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

	if err := ProcessImageRequestSync(request.ID); err != nil {
		// Same reasoning as CreateImageProcessingRequest: the outcome is
		// already on the row itself, this is purely for operator
		// visibility.
		facades.Log().With(map[string]any{"request_id": request.ID, "error": err.Error()}).
			Warning("image processing finished with an error")
	}

	return &request, nil
}
