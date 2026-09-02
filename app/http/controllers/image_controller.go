package controllers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/goravel/framework/contracts/http"

	"goravel/app/facades"
	"goravel/app/http/requests"
	"goravel/app/http/resources"
	"goravel/app/models"
	"goravel/app/services"
	"goravel/app/support/imageconfig"
)

type ImageController struct{}

func NewImageController() *ImageController {
	return &ImageController{}
}

// Store handles POST /api/v1/images. It accepts either:
//
//   - Content-Type: application/json - the original shape, unchanged:
//     { "image_url": "...", "sizes": [...] }
//   - Content-Type: multipart/form-data - for local file uploads: a "data"
//     text field holding that same JSON (image_url now optional there),
//     plus an optional "image" file field. If both an image_url and a file
//     are present, the uploaded file takes precedence and image_url is
//     ignored.
//
// Either way: validate, persist a pending request + its requested sizes,
// dispatch the processing job, return immediately. All the expensive work
// happens later in the worker - see app/jobs.
func (c *ImageController) Store(ctx http.Context) http.Response {
	if isMultipart(ctx) {
		return c.storeMultipart(ctx)
	}
	return c.storeJSON(ctx)
}

func isMultipart(ctx http.Context) bool {
	return strings.HasPrefix(ctx.Request().Header("Content-Type"), "multipart/form-data")
}

func sizeRules(imageURLRule string) map[string]any {
	rules := map[string]any{
		"sizes":                 fmt.Sprintf("required|array|min:1|max:%d", imageconfig.MaxSizesPerRequest()),
		"sizes.*.width":         fmt.Sprintf("required|integer|min:1|max:%d", imageconfig.MaxImageWidth()),
		"sizes.*.height":        fmt.Sprintf("required|integer|min:1|max:%d", imageconfig.MaxImageHeight()),
		"sizes.*.mode":          "string|in:contain,fit,cover,crop,fill",
		"sizes.*.allow_upscale": "bool",
		"sizes.*.quality":       fmt.Sprintf("integer|min:%d|max:%d", imageconfig.QualityMin(), imageconfig.QualityMax()),
	}
	if imageURLRule != "" {
		rules["image_url"] = imageURLRule
	}
	return rules
}

// storeJSON is the original, unchanged JSON-body path.
func (c *ImageController) storeJSON(ctx http.Context) http.Response {
	validator, err := ctx.Request().Validate(sizeRules("required|url"))
	if err != nil {
		return ctx.Response().Status(http.StatusInternalServerError).Json(http.Json{"error": "validation failed to run"})
	}
	if validator.Fails() {
		return ctx.Response().Status(http.StatusUnprocessableEntity).Json(http.Json{"errors": validator.Errors().All()})
	}

	var body requests.CreateImageRequestBody
	if err := validator.Bind(&body); err != nil {
		return ctx.Response().Status(http.StatusUnprocessableEntity).Json(http.Json{"error": "invalid request body"})
	}

	request, err := services.CreateImageProcessingRequest(body)
	if err != nil {
		facades.Log().With(map[string]any{"error": err.Error()}).Error("failed to create image processing request")
		return ctx.Response().Status(http.StatusInternalServerError).Json(http.Json{"error": "failed to create processing request"})
	}

	return ctx.Response().Status(http.StatusAccepted).Json(resources.RequestAccepted(request))
}

// storeMultipart handles the multipart/form-data path: a "data" field
// (JSON string, same shape as the JSON body) plus an optional "image" file.
func (c *ImageController) storeMultipart(ctx http.Context) http.Response {
	file, fileErr := ctx.Request().File("image")
	hasFile := fileErr == nil && file != nil

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

	// image_url is only required when no file was uploaded; if a file *is*
	// present it takes precedence, so image_url (if also given) is left
	// unvalidated/ignored rather than enforced.
	imageURLRule := ""
	if !hasFile {
		imageURLRule = "required|url"
	}

	validator, err := facades.Validation().Make(ctx, parsed, sizeRules(imageURLRule))
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

	var request *models.ImageProcessingRequest
	if hasFile {
		if size, sizeErr := file.Size(); sizeErr == nil && size > imageconfig.MaxImageFileSize() {
			return ctx.Response().Status(http.StatusUnprocessableEntity).Json(http.Json{
				"errors": http.Json{"image": []string{"the uploaded file exceeds the configured maximum size"}},
			})
		}
		request, err = services.CreateImageProcessingRequestFromUpload(file, body.Sizes)
	} else {
		request, err = services.CreateImageProcessingRequest(body)
	}
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
