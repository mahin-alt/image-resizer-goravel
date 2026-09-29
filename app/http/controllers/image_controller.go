package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/goravel/framework/contracts/http"

	"goravel/app/facades"
	"goravel/app/http/requests"
	"goravel/app/http/resources"
	"goravel/app/models"
	"goravel/app/services"
	"goravel/app/support/imageconfig"
	"goravel/app/support/statusbus"
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

	return ctx.Response().Status(http.StatusAccepted).Json(resources.RequestAccepted(request))
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
// Show returns it. Shared with Events below so both endpoints can never
// drift in what they consider a request's state to be.
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

// openWaitRequests bounds how many GET /images/{id}/wait calls this process
// blocks on at once - see imageconfig.StatusWaitMaxConnections. Each one
// holds a goroutine for up to imageconfig.StatusWaitDuration, so this cap is
// what keeps a burst of clients from growing that unbounded.
var openWaitRequests atomic.Int64

// Wait handles GET /api/v1/images/{id}/wait: a long-poll that blocks until
// this request's status changes, reaches a terminal status, or
// imageconfig.StatusWaitDuration elapses - then returns the current detail
// as a single, normal JSON response, exactly like Show. The frontend calls
// this in a loop (see watchImageRequest) instead of polling Show on a fixed
// timer: most calls block for a while and return the instant something
// actually changes, so a client watching an in-progress request costs
// roughly one request per status change rather than one per fixed interval.
//
// This is deliberately NOT a real streaming response (Server-Sent Events,
// chunked push, etc.), even though that was tried first: Goravel's global
// request-timeout middleware (gin-contrib/timeout, wired in via
// goravel/gin's engine.Use(), which nothing in this app can opt a route out
// of) swaps in a fully-buffered ResponseWriter for every request and only
// copies it to the real connection once the handler returns. Anything
// written incrementally during a long-held-open response sits in that
// buffer, invisible to the client, until the connection ends anyway - which
// defeats the entire point of a push stream. A long-poll (one request in,
// one complete JSON response out, repeated by the client) has no such
// problem: it already behaves exactly like every other endpoint here, one
// full response written after the handler decides it's done.
func (c *ImageController) Wait(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
	}
	requestID := uint(id)

	if openWaitRequests.Add(1) > int64(imageconfig.StatusWaitMaxConnections()) {
		openWaitRequests.Add(-1)
		// Rather than making this client queue behind a full budget of
		// other long-polls, just answer immediately with whatever the
		// current state is - the frontend's loop simply calls again.
		detail, ok := loadRequestDetail(requestID)
		if !ok {
			return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
		}
		return ctx.Response().Success().Json(detail)
	}
	defer openWaitRequests.Add(-1)

	// Subscribe before the first load, not after, so a status change that
	// happens concurrently with it can never fall in a gap between "read
	// current state" and "start listening for the next change" - it either
	// lands in this very read, or it's already queued on `changed` by the
	// time the select below runs.
	changed, unsubscribe := statusbus.Subscribe(requestID)
	defer unsubscribe()

	detail, ok := loadRequestDetail(requestID)
	if !ok {
		return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
	}
	if models.IsTerminalStatus(detail.Status) {
		return ctx.Response().Success().Json(detail)
	}

	timer := time.NewTimer(imageconfig.StatusWaitDuration())
	defer timer.Stop()

	select {
	case <-ctx.Request().Origin().Context().Done():
		// The client gave up waiting (navigated away, tab/component
		// closed) - nothing left to respond to, and nothing reads this
		// response body anyway.
		return ctx.Response().NoContent()
	case <-timer.C:
		// Nothing changed within the wait budget - hand back whatever the
		// status still is (same as calling Show); the frontend's loop
		// just calls again immediately.
		return ctx.Response().Success().Json(detail)
	case <-changed:
		updated, ok := loadRequestDetail(requestID)
		if !ok {
			return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
		}
		return ctx.Response().Success().Json(updated)
	}
}

// Retry handles POST /api/v1/images/{id}/retry. Only requests currently
// "failed" or "partially_completed" can be retried - see
// services.RetryImageProcessingRequest.
func (c *ImageController) Retry(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctx.Response().Status(http.StatusNotFound).Json(http.Json{"error": "not found"})
	}

	request, err := services.RetryImageProcessingRequest(uint(id))
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

	return ctx.Response().Status(http.StatusAccepted).Json(resources.RequestAccepted(request))
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
