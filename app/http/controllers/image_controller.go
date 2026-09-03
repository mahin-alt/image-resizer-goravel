package controllers

import (
	"encoding/json"
	"fmt"

	"github.com/goravel/framework/contracts/http"

	"goravel/app/facades"
	"goravel/app/http/requests"
	"goravel/app/http/resources"
	"goravel/app/models"
	"goravel/app/services"
	"goravel/app/storage"
	"goravel/app/support/imageconfig"
)

type ImageController struct{}

func NewImageController() *ImageController {
	return &ImageController{}
}

// Store handles POST /api/v1/images. The source image is always a local
// file upload (Content-Type: multipart/form-data): an "image" file field,
// plus a "data" text field holding { "sizes": [...] } as a JSON string.
//
// Validate, store the upload, persist a pending request + its requested
// sizes, dispatch the processing job, return immediately. All the expensive
// work happens later in the worker - see app/jobs.
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

	rules := map[string]any{
		"sizes":                 fmt.Sprintf("required|array|min:1|max:%d", imageconfig.MaxSizesPerRequest()),
		"sizes.*.width":         fmt.Sprintf("required|integer|min:1|max:%d", imageconfig.MaxImageWidth()),
		"sizes.*.height":        fmt.Sprintf("required|integer|min:1|max:%d", imageconfig.MaxImageHeight()),
		"sizes.*.mode":          "string|in:contain,fit,cover,crop,fill",
		"sizes.*.allow_upscale": "bool",
		"sizes.*.quality":       fmt.Sprintf("integer|min:%d|max:%d", imageconfig.QualityMin(), imageconfig.QualityMax()),
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

	request, err := services.CreateImageProcessingRequest(file, body.Sizes)
	if err != nil {
		facades.Log().With(map[string]any{"error": err.Error()}).Error("failed to create image processing request")
		return ctx.Response().Status(http.StatusInternalServerError).Json(http.Json{"error": "failed to create processing request"})
	}

	return ctx.Response().Status(http.StatusAccepted).Json(resources.RequestAccepted(request))
}

// Show handles GET /api/v1/images/{id}.
func (c *ImageController) Show(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
	}

	var request models.ImageProcessingRequest
	if err := facades.Orm().Query().Where("id", id).First(&request); err != nil || request.ID == 0 {
		return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
	}

	var sizes []models.ImageProcessingRequestSize
	_ = facades.Orm().Query().Where("image_processing_request_id", request.ID).Find(&sizes)

	var outputs []models.ImageOutput
	_ = facades.Orm().Query().Where("image_processing_request_id", request.ID).Find(&outputs)

	return ctx.Response().Success().Json(resources.RequestDetail(&request, sizes, outputs))
}

// Download handles GET /api/v1/images/{id}/outputs/{outputId}/download.
// Unlike the plain "url" field (a static file URL a browser renders inline),
// this sets Content-Disposition: attachment so it always triggers a browser
// download instead.
func (c *ImageController) Download(ctx http.Context) http.Response {
	requestID := ctx.Request().RouteInt("id")
	outputID := ctx.Request().RouteInt("outputId")
	if requestID <= 0 || outputID <= 0 {
		return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
	}

	var output models.ImageOutput
	if err := facades.Orm().Query().Where("id", outputID).First(&output); err != nil ||
		output.ID == 0 || output.ImageProcessingRequestID != uint(requestID) {
		return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
	}

	if !storage.OutputExists(output.StoragePath) {
		return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "file no longer available"})
	}

	content, err := storage.GetOutput(output.StoragePath)
	if err != nil {
		facades.Log().With(map[string]any{"error": err.Error(), "output_id": output.ID}).
			Error("failed to fetch output from storage")
		return ctx.Response().Status(http.StatusInternalServerError).Json(http.Json{"error": "failed to fetch file"})
	}

	filename := fmt.Sprintf("image-%dx%d.webp", output.Width, output.Height)
	return ctx.Response().
		Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename)).
		Data(http.StatusOK, "image/webp", content)
}
