// Package resources shapes models into the JSON the API actually returns,
// keeping that mapping out of the controller.
//
// These are plain structs (not http.Json/map[string]any) specifically so
// the JSON key order is whatever we declare below - encoding/json marshals
// struct fields in declaration order, but alphabetizes map keys, which is
// why this used to come out as completed_at/created_at/id/... regardless of
// insertion order.
package resources

import (
	"fmt"

	"github.com/goravel/framework/support/carbon"

	"goravel/app/facades"
	"goravel/app/models"
	"goravel/app/storage"
)

type RequestAcceptedResponse struct {
	Status string `json:"status"`
	ID     uint   `json:"id"`
}

func RequestAccepted(r *models.ImageProcessingRequest) *RequestAcceptedResponse {
	return &RequestAcceptedResponse{
		Status: r.Status,
		ID:     r.ID,
	}
}

type ImageOutputResponse struct {
	URL         string `json:"url"`
	DownloadURL string `json:"download_url"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Mode        string `json:"mode"`
	Format      string `json:"format"`
	FileSize    int64  `json:"file_size"`
}

type SizeErrorResponse struct {
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Message string `json:"message"`
}

type SourceImageResponse struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type RequestDetailResponse struct {
	Status       string                `json:"status"`
	ID           uint                  `json:"id"`
	InputType    string                `json:"input_type"`
	SourceURL    string                `json:"source_url,omitempty"`
	SourceImage  *SourceImageResponse  `json:"source_image,omitempty"`
	Images       []ImageOutputResponse `json:"images"`
	ErrorMessage string                `json:"error_message,omitempty"`
	Errors       []SizeErrorResponse   `json:"errors,omitempty"`
	CreatedAt    *carbon.DateTime      `json:"created_at"`
	CompletedAt  *carbon.DateTime      `json:"completed_at,omitempty"`
}

func RequestDetail(r *models.ImageProcessingRequest, sizes []models.ImageProcessingRequestSize, outputs []models.ImageOutput) *RequestDetailResponse {
	outputsBySize := make(map[uint]models.ImageOutput, len(outputs))
	for _, o := range outputs {
		outputsBySize[o.ImageProcessingRequestSizeID] = o
	}

	images := make([]ImageOutputResponse, 0, len(outputs))
	var errs []SizeErrorResponse
	for _, s := range sizes {
		switch s.Status {
		case models.SizeStatusCompleted:
			if o, ok := outputsBySize[s.ID]; ok {
				images = append(images, ImageOutputResponse{
					URL:         storage.Url(o.StoragePath),
					DownloadURL: downloadURL(r.ID, o.ID),
					Width:       o.Width,
					Height:      o.Height,
					Mode:        o.Mode,
					Format:      o.Format,
					FileSize:    o.FileSize,
				})
			}
		case models.SizeStatusFailed:
			msg := "processing failed"
			if s.ErrorMessage != nil {
				msg = *s.ErrorMessage
			}
			errs = append(errs, SizeErrorResponse{
				Width:   s.Width,
				Height:  s.Height,
				Message: msg,
			})
		}
	}

	resp := &RequestDetailResponse{
		Status:    r.Status,
		ID:        r.ID,
		InputType: r.InputType,
		Images:    images,
		CreatedAt: r.CreatedAt,
	}
	// source_url only applies to input_type "url" requests - omit it
	// entirely for uploads instead of showing an empty string.
	if r.InputType == models.InputTypeURL {
		resp.SourceURL = r.SourceURL
	}
	// Only known once the worker has downloaded/read and inspected the
	// source (i.e. not while status is still "pending").
	if r.SourceWidth != nil && r.SourceHeight != nil {
		resp.SourceImage = &SourceImageResponse{Width: *r.SourceWidth, Height: *r.SourceHeight}
	}
	if r.ErrorMessage != nil {
		resp.ErrorMessage = *r.ErrorMessage
	}
	if len(errs) > 0 {
		resp.Errors = errs
	}
	if r.CompletedAt != nil {
		resp.CompletedAt = r.CompletedAt
	}
	return resp
}

// downloadURL points at the download-forcing endpoint (Content-Disposition:
// attachment - see ImageController.Download), as opposed to URL above,
// which is the plain static file URL a browser will happily render inline.
func downloadURL(requestID, outputID uint) string {
	base, _ := facades.Config().Env("APP_URL", "").(string)
	return fmt.Sprintf("%s/api/v1/images/%d/outputs/%d/download", base, requestID, outputID)
}
