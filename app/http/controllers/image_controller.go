package controllers

import (
	"fmt"

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

// Store handles POST /api/v1/images: validate, persist a pending request +
// its requested sizes, dispatch the processing job, return immediately. All
// the expensive work happens later in the worker - see app/jobs.
func (c *ImageController) Store(ctx http.Context) http.Response {
	maxSizes := imageconfig.MaxSizesPerRequest()

	validator, err := ctx.Request().Validate(map[string]any{
		"image_url":             "required|url",
		"sizes":                 fmt.Sprintf("required|array|min:1|max:%d", maxSizes),
		"sizes.*.width":         fmt.Sprintf("required|integer|min:1|max:%d", imageconfig.MaxImageWidth()),
		"sizes.*.height":        fmt.Sprintf("required|integer|min:1|max:%d", imageconfig.MaxImageHeight()),
		"sizes.*.mode":          "string|in:contain,fit,cover,crop,fill",
		"sizes.*.allow_upscale": "bool",
		"sizes.*.quality":       fmt.Sprintf("integer|min:%d|max:%d", imageconfig.QualityMin(), imageconfig.QualityMax()),
	})
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
