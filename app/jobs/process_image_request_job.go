package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"goravel/app/facades"
	"goravel/app/models"
	"goravel/app/storage"
	"goravel/app/support/imageconfig"
	"goravel/internal/download"
	"goravel/internal/imageprocessing"

	"github.com/goravel/framework/support/carbon"
)

// permanentError marks a failure as non-retryable (bad input), as opposed to
// a transient one (network blip) that Goravel's default retry/backoff should
// keep retrying.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// ProcessImageRequestJob is the single job that handles one
// ImageProcessingRequest end to end: download the source once, decode it
// once, generate every requested size from that one decode, persist each
// output, and finalize the request's status. See the plan's "one job per
// request" rationale for why this isn't split per-size.
type ProcessImageRequestJob struct{}

func (r *ProcessImageRequestJob) Signature() string {
	return "process_image_request"
}

// ShouldRetry stops Goravel from retrying validation-shaped failures
// (unsupported format, corrupt image, bad URL) - those will never succeed on
// a retry. Transient failures (timeouts, network errors) fall through to the
// framework's default retry/backoff behavior.
func (r *ProcessImageRequestJob) ShouldRetry(err error, attempt, maxTries int) (bool, time.Duration) {
	var perm *permanentError
	if errors.As(err, &perm) {
		return false, 0
	}
	return attempt < maxTries, time.Duration(attempt) * 5 * time.Second
}

func (r *ProcessImageRequestJob) Handle(args ...any) error {
	if len(args) != 1 {
		return permanent(fmt.Errorf("process_image_request: expected 1 arg, got %d", len(args)))
	}
	requestID, ok := args[0].(uint)
	if !ok {
		return permanent(fmt.Errorf("process_image_request: expected uint arg, got %T", args[0]))
	}

	var request models.ImageProcessingRequest
	if err := facades.Orm().Query().Where("id", requestID).First(&request); err != nil {
		return permanent(fmt.Errorf("load request %d: %w", requestID, err))
	}
	if request.ID == 0 {
		return permanent(fmt.Errorf("request %d not found", requestID))
	}

	// Idempotency: if a previous attempt already finished this request,
	// don't redo the work. This can happen if the job succeeded but the
	// queue driver re-delivered it (e.g. after a worker crash before ack).
	switch request.Status {
	case models.RequestStatusCompleted, models.RequestStatusPartiallyCompleted, models.RequestStatusFailed:
		return nil
	}

	startedAt := carbon.NewDateTime(carbon.Now())
	if _, err := facades.Orm().Query().Model(&request).Update(map[string]any{
		"status":     models.RequestStatusProcessing,
		"started_at": startedAt,
	}); err != nil {
		return fmt.Errorf("mark request processing: %w", err)
	}

	var sizes []models.ImageProcessingRequestSize
	if err := facades.Orm().Query().Where("image_processing_request_id", request.ID).Find(&sizes); err != nil {
		return fmt.Errorf("load requested sizes: %w", err)
	}
	if len(sizes) == 0 {
		return permanent(fmt.Errorf("request %d has no requested sizes", request.ID))
	}

	ctx := context.Background()

	result, err := download.Download(ctx, request.SourceURL)
	if err != nil {
		if isPermanentDownloadError(err) {
			return r.fail(&request, permanent(err))
		}
		return r.fail(&request, err) // let it retry
	}
	defer os.Remove(result.Path)

	processor := imageprocessing.NewGovipsImageProcessor()

	info, err := processor.Inspect(ctx, result.Path)
	if err != nil {
		return r.fail(&request, permanent(fmt.Errorf("invalid image: %w", err)))
	}
	if info.Width > imageconfig.MaxImageWidth() || info.Height > imageconfig.MaxImageHeight() {
		return r.fail(&request, permanent(fmt.Errorf(
			"source image %dx%d exceeds the configured maximum of %dx%d",
			info.Width, info.Height, imageconfig.MaxImageWidth(), imageconfig.MaxImageHeight())))
	}

	specs := make([]imageprocessing.SizeSpec, 0, len(sizes))
	for _, s := range sizes {
		if int64(s.Width)*int64(s.Height) > imageconfig.MaxTotalOutputPixels() {
			markSizeFailed(s.ID, fmt.Sprintf("%dx%d exceeds the configured maximum output pixel count", s.Width, s.Height))
			continue
		}
		specs = append(specs, imageprocessing.SizeSpec{
			RequestSizeID: s.ID,
			Width:         s.Width,
			Height:        s.Height,
			Mode:          s.Mode,
			AllowUpscale:  s.AllowUpscale,
			Quality:       s.Quality,
		})
	}

	outputs, sizeErrs, err := processor.ProcessSizes(ctx, result.Path, specs)
	if err != nil {
		return r.fail(&request, permanent(fmt.Errorf("processing failed: %w", err)))
	}

	for _, se := range sizeErrs {
		markSizeFailed(se.RequestSizeID, se.Err.Error())
	}

	successCount := 0
	for _, out := range outputs {
		if err := r.persistOutput(&request, out); err != nil {
			markSizeFailed(out.RequestSizeID, err.Error())
			continue
		}
		successCount++
	}

	finalStatus := models.RequestStatusFailed
	switch {
	case successCount == len(sizes):
		finalStatus = models.RequestStatusCompleted
	case successCount > 0:
		finalStatus = models.RequestStatusPartiallyCompleted
	}

	_, err = facades.Orm().Query().Model(&request).Update(map[string]any{
		"status":       finalStatus,
		"completed_at": carbon.NewDateTime(carbon.Now()),
	})
	return err
}

// persistOutput is idempotent: a retry that re-runs this size will find the
// existing ImageOutput row (unique on request_size_id) and skip re-writing
// it, rather than creating a duplicate.
func (r *ProcessImageRequestJob) persistOutput(request *models.ImageProcessingRequest, out imageprocessing.Output) error {
	var existing models.ImageOutput
	_ = facades.Orm().Query().Where("image_processing_request_size_id", out.RequestSizeID).First(&existing)
	if existing.ID != 0 {
		markSizeCompleted(out.RequestSizeID)
		return nil
	}

	output := &models.ImageOutput{
		ImageProcessingRequestID:     request.ID,
		ImageProcessingRequestSizeID: out.RequestSizeID,
		Width:                        out.Width,
		Height:                       out.Height,
		Mode:                         out.Mode,
		Format:                       "webp",
		FileSize:                     int64(len(out.Bytes)),
		ExpiresAt:                    carbon.NewDateTime(carbon.Now().AddHours(imageconfig.RetentionHours())),
	}
	if err := facades.Orm().Query().Create(output); err != nil {
		return fmt.Errorf("create output row: %w", err)
	}

	path := storage.OutputPath(request.ID, output.ID)
	if err := storage.Put(path, out.Bytes); err != nil {
		// Roll back the row so a retry can cleanly recreate it rather than
		// leaving a DB row with no backing file.
		_, _ = facades.Orm().Query().Delete(output)
		return fmt.Errorf("store output file: %w", err)
	}

	if _, err := facades.Orm().Query().Model(output).Update("storage_path", path); err != nil {
		return fmt.Errorf("record storage path: %w", err)
	}

	markSizeCompleted(out.RequestSizeID)
	return nil
}

func (r *ProcessImageRequestJob) fail(request *models.ImageProcessingRequest, err error) error {
	msg := err.Error()
	_, _ = facades.Orm().Query().Model(request).Update(map[string]any{
		"status":        models.RequestStatusFailed,
		"error_message": msg,
		"completed_at":  carbon.NewDateTime(carbon.Now()),
	})
	return err
}

func markSizeFailed(sizeID uint, message string) {
	_, _ = facades.Orm().Query().Model(&models.ImageProcessingRequestSize{}).
		Where("id", sizeID).
		Update(map[string]any{"status": models.SizeStatusFailed, "error_message": message})
}

func markSizeCompleted(sizeID uint) {
	_, _ = facades.Orm().Query().Model(&models.ImageProcessingRequestSize{}).
		Where("id", sizeID).
		Update(map[string]any{"status": models.SizeStatusCompleted})
}

func isPermanentDownloadError(err error) bool {
	return errors.Is(err, download.ErrUnsupportedScheme) ||
		errors.Is(err, download.ErrBadStatus) ||
		errors.Is(err, download.ErrResponseTooLarge) ||
		errors.Is(err, download.ErrTooManyRedirects)
}
