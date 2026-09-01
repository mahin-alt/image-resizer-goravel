// Package services holds application-level orchestration that controllers
// call into. Keeping it here (rather than in the controller) is what lets
// controllers stay thin per the architecture plan.
package services

import (
	"fmt"

	"github.com/goravel/framework/contracts/database/orm"
	"github.com/goravel/framework/contracts/queue"

	"goravel/app/facades"
	"goravel/app/http/requests"
	"goravel/app/jobs"
	"goravel/app/models"
	"goravel/app/support/imageconfig"
)

// CreateImageProcessingRequest persists a pending ImageProcessingRequest plus
// one ImageProcessingRequestSize per requested size, then dispatches the
// single processing job for it (see the "one job per request" decision in
// the architecture plan). It does not touch the network or govips - that all
// happens in the worker.
func CreateImageProcessingRequest(body requests.CreateImageRequestBody) (*models.ImageProcessingRequest, error) {
	request := &models.ImageProcessingRequest{
		SourceURL: body.ImageURL,
		Status:    models.RequestStatusPending,
	}

	err := facades.Orm().Transaction(func(tx orm.Query) error {
		if err := tx.Create(request); err != nil {
			return fmt.Errorf("create processing request: %w", err)
		}

		for _, s := range body.Sizes {
			mode := s.Mode
			if mode == "" {
				mode = models.ResizeModeContain
			}

			size := &models.ImageProcessingRequestSize{
				ImageProcessingRequestID: request.ID,
				Width:                    s.Width,
				Height:                   s.Height,
				Mode:                     mode,
				AllowUpscale:             imageconfig.ResolveUpscale(s.AllowUpscale),
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

	if err := facades.Queue().
		Job(&jobs.ProcessImageRequestJob{}, []queue.Arg{{Type: "uint", Value: request.ID}}).
		OnQueue(imageconfig.ProcessingQueue()).
		Dispatch(); err != nil {
		return nil, fmt.Errorf("dispatch processing job: %w", err)
	}

	return request, nil
}
