package controllers

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/goravel/framework/contracts/http"

	"goravel/app/facades"
	"goravel/app/http/requests"
	"goravel/app/http/resources"
	"goravel/app/models"
	"goravel/app/services"
	"goravel/app/support/imageconfig"
	"goravel/internal/imageprocessing"
)

type ImageController struct{}

func NewImageController() *ImageController {
	return &ImageController{}
}

// Store handles POST /api/v1/images. The source image is always a local
// file upload (Content-Type: multipart/form-data): an "image" file field,
// plus a "data" text field holding { "sizes": [...] } as a JSON string.
//
// Validate, store the upload, then process every requested size inline
// before responding - see services.CreateImageProcessingRequest and
// services.ProcessImageRequestSync. This blocks for as long as processing
// takes (bounded by http.request_timeout, see config/http.go) and returns
// the finished result directly: one call in, final images out, no
// separate status endpoint to poll. Concurrency across simultaneous
// uploads is capped by services.ProcessImageRequestSync's own semaphore
// (MAX_CONCURRENT_IMAGE_JOBS) rather than a queue worker pool.
func (c *ImageController) Store(ctx http.Context) http.Response {
	file, fileErr := ctx.Request().File("image")
	if fileErr != nil || file == nil {
		return ctx.Response().Status(http.StatusUnprocessableEntity).Json(http.Json{
			"errors": http.Json{"image": []string{"the image field is required"}},
		})
	}

	if size, sizeErr := file.Size(); sizeErr == nil && size > imageconfig.MaxImageFileSize() {
		return ctx.Response().Status(http.StatusUnprocessableEntity).Json(http.Json{
			"errors": http.Json{"image": []string{"the uploaded file exceeds the configured maximum size"}},
		})
	}

	dataStr := ctx.Request().Input("data")
	if dataStr == "" {
		return ctx.Response().Status(http.StatusUnprocessableEntity).Json(http.Json{
			"errors": http.Json{"data": []string{"the data field is required"}},
		})
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(dataStr), &parsed); err != nil {
		return ctx.Response().Status(http.StatusUnprocessableEntity).Json(http.Json{
			"errors": http.Json{"data": []string{"the data field must be valid JSON"}},
		})
	}

	// Width/height are capped at whichever is smaller: the app's own
	// configured business limit (imageconfig.MaxImage{Width,Height}), or
	// libvips' own hard ceiling (imageprocessing.MaxSupportedDimension) -
	// no request can demand a size the library could never produce,
	// regardless of how MAX_IMAGE_WIDTH/MAX_IMAGE_HEIGHT are configured.
	// "min:1" (combined with "integer") already rejects zero and negative
	// values.
	maxWidth := imageconfig.MaxImageWidth()
	if maxWidth > imageprocessing.MaxSupportedDimension {
		maxWidth = imageprocessing.MaxSupportedDimension
	}
	maxHeight := imageconfig.MaxImageHeight()
	if maxHeight > imageprocessing.MaxSupportedDimension {
		maxHeight = imageprocessing.MaxSupportedDimension
	}

	rules := map[string]any{
		"sizes":           fmt.Sprintf("required|array|min:1|max:%d", imageconfig.MaxSizesPerRequest()),
		"sizes.*.width":   fmt.Sprintf("required|integer|min:1|max:%d", maxWidth),
		"sizes.*.height":  fmt.Sprintf("required|integer|min:1|max:%d", maxHeight),
		"sizes.*.mode":    "string|in:contain,fit,cover,crop,fill",
		"sizes.*.quality": fmt.Sprintf("integer|min:%d|max:%d", imageconfig.QualityMin(), imageconfig.QualityMax()),
	}

	validator, err := facades.Validation().Make(ctx, parsed, rules)
	if err != nil {
		return ctx.Response().Status(http.StatusInternalServerError).Json(http.Json{"error": "validation failed to run"})
	}
	if validator.Fails() {
		return ctx.Response().Status(http.StatusUnprocessableEntity).Json(http.Json{"errors": validator.Errors().All()})
	}

	var body requests.CreateImageRequestBody
	if err := validator.Bind(&body); err != nil {
		return ctx.Response().Status(http.StatusUnprocessableEntity).Json(http.Json{"error": "invalid data field"})
	}

	// A request asking for the same width x height more than once is never
	// valid (it can only ever produce a unique output per size - see the
	// unique index on image_processing_request_sizes) - reject the whole
	// request rather than silently collapsing/dropping the duplicate.
	if dupe := firstDuplicateSize(body.Sizes); dupe != "" {
		return ctx.Response().Status(http.StatusUnprocessableEntity).Json(http.Json{
			"errors": http.Json{"sizes": []string{fmt.Sprintf("size %s was requested more than once", dupe)}},
		})
	}

	request, err := services.CreateImageProcessingRequest(file, body.Sizes)
	if err != nil {
		facades.Log().With(map[string]any{"error": err.Error()}).Error("failed to create image processing request")
		return ctx.Response().Status(http.StatusInternalServerError).Json(http.Json{"error": "failed to create processing request"})
	}

	// Re-read rather than trust `request`: CreateImageProcessingRequest
	// already processed it synchronously by the time it returns, but that
	// happened through separate DB writes this in-memory copy was never
	// updated from.
	detail, ok := loadRequestDetail(request.ID)
	if !ok {
		facades.Log().With(map[string]any{"request_id": request.ID}).
			Error("image processing request vanished immediately after creation")
		return ctx.Response().Status(http.StatusInternalServerError).Json(http.Json{"error": "failed to load processed request"})
	}

	return ctx.Response().Success().Json(detail)
}

// Show handles GET /api/v1/images/{id}.
func (c *ImageController) Show(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
	}

	detail, ok := loadRequestDetail(uint(id))
	if !ok {
		return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
	}

	return ctx.Response().Success().Json(detail)
}

// loadRequestDetail loads and shapes one request's current state exactly as
// Show returns it. Shared with Store and Retry (both of which re-read the
// request after processing it synchronously) so all three endpoints can
// never drift in what they consider a request's state to be.
func loadRequestDetail(id uint) (*resources.RequestDetailResponse, bool) {
	var request models.ImageProcessingRequest
	if err := facades.Orm().Query().Where("id", id).First(&request); err != nil || request.ID == 0 {
		return nil, false
	}

	var sizes []models.ImageProcessingRequestSize
	_ = facades.Orm().Query().Where("image_processing_request_id", request.ID).Find(&sizes)

	var outputs []models.ImageOutput
	_ = facades.Orm().Query().Where("image_processing_request_id", request.ID).Find(&outputs)

	return resources.RequestDetail(&request, sizes, outputs), true
}

// Retry handles POST /api/v1/images/{id}/retry. Only requests currently
// "failed" or "partially_completed" can be retried - see
// services.RetryImageProcessingRequest. Like Store, this now blocks until
// reprocessing finishes and returns the final result directly.
func (c *ImageController) Retry(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
	}
	requestID := uint(id)

	_, err := services.RetryImageProcessingRequest(requestID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrRequestNotFound):
			return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
		case errors.Is(err, services.ErrRequestNotRetryable):
			return ctx.Response().Status(http.StatusConflict).Json(http.Json{"error": err.Error()})
		default:
			facades.Log().With(map[string]any{"error": err.Error()}).Error("failed to retry image processing request")
			return ctx.Response().Status(http.StatusInternalServerError).Json(http.Json{"error": "failed to retry processing request"})
		}
	}

	detail, ok := loadRequestDetail(requestID)
	if !ok {
		facades.Log().With(map[string]any{"request_id": requestID}).
			Error("image processing request vanished immediately after retry")
		return ctx.Response().Status(http.StatusInternalServerError).Json(http.Json{"error": "failed to load processed request"})
	}

	return ctx.Response().Success().Json(detail)
}

// firstDuplicateSize returns "{width}x{height}" for the first width/height
// pair that appears more than once in sizes, or "" if all are distinct.
func firstDuplicateSize(sizes []requests.RequestedSize) string {
	seen := make(map[[2]int]bool, len(sizes))
	for _, s := range sizes {
		key := [2]int{s.Width, s.Height}
		if seen[key] {
			return fmt.Sprintf("%dx%d", s.Width, s.Height)
		}
		seen[key] = true
	}
	return ""
}
