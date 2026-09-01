// Package resources shapes models into the JSON the API actually returns,
// keeping that mapping out of the controller.
package resources

import (
	"github.com/goravel/framework/contracts/http"

	"goravel/app/models"
	"goravel/app/storage"
)

func RequestAccepted(r *models.ImageProcessingRequest) http.Json {
	return http.Json{
		"id":     r.ID,
		"status": r.Status,
	}
}

func RequestDetail(r *models.ImageProcessingRequest, sizes []models.ImageProcessingRequestSize, outputs []models.ImageOutput) http.Json {
	outputsBySize := make(map[uint]models.ImageOutput, len(outputs))
	for _, o := range outputs {
		outputsBySize[o.ImageProcessingRequestSizeID] = o
	}

	images := make([]http.Json, 0, len(outputs))
	var errs []http.Json
	for _, s := range sizes {
		switch s.Status {
		case models.SizeStatusCompleted:
			if o, ok := outputsBySize[s.ID]; ok {
				images = append(images, http.Json{
					"width":     o.Width,
					"height":    o.Height,
					"mode":      o.Mode,
					"format":    o.Format,
					"url":       storage.Url(o.StoragePath),
					"file_size": o.FileSize,
				})
			}
		case models.SizeStatusFailed:
			msg := "processing failed"
			if s.ErrorMessage != nil {
				msg = *s.ErrorMessage
			}
			errs = append(errs, http.Json{
				"width":   s.Width,
				"height":  s.Height,
				"message": msg,
			})
		}
	}

	body := http.Json{
		"id":         r.ID,
		"status":     r.Status,
		"source_url": r.SourceURL,
		"created_at": r.CreatedAt,
		"images":     images,
	}
	if r.CompletedAt != nil {
		body["completed_at"] = r.CompletedAt
	}
	if r.ErrorMessage != nil {
		body["error_message"] = *r.ErrorMessage
	}
	if len(errs) > 0 {
		body["errors"] = errs
	}
	return body
}
