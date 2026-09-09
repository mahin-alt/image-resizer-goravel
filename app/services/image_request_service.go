// Package services holds application-level orchestration that controllers
// call into. Keeping it here (rather than in the controller) is what lets
// controllers stay thin per the architecture plan.
package services

import (
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
