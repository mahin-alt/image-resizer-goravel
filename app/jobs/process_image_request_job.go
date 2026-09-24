package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"goravel/app/facades"
	"goravel/app/models"
	"goravel/app/storage"
	"goravel/app/support/imageconfig"
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
// ImageProcessingRequest end to end: decode the uploaded source once,
// generate every requested size from that one decode, persist each output,
// and finalize the request's status. See the plan's "one job per request"
// rationale for why this isn't split per-size.
type ProcessImageRequestJob struct{}

func (r *ProcessImageRequestJob) Signature() string {
	return "process_image_request"
}

// ShouldRetry stops Goravel from retrying validation-shaped failures
// (unsupported format, corrupt/missing image) - those will never succeed on
// a retry. Transient failures fall through to the framework's default
// retry/backoff behavior.
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
	if err := updateRequest(request.ID, map[string]any{
		"status":            models.RequestStatusProcessing,
		"started_at":        startedAt,
		"last_heartbeat_at": startedAt,
	}); err != nil {
		return fmt.Errorf("mark request processing: %w", err)
	}

	// Keep proving to services.SweepStaleProcessingRequests that this
	// request's worker is still alive for as long as Handle is doing real
	// work below. If the process dies (crash, forced shutdown) the ticks
	// simply stop, the heartbeat goes stale, and the sweep reclaims it -
	// there's no error path to hook that case into since nothing here gets
	// to run when the process itself disappears.
	stopHeartbeat := r.startHeartbeat(request.ID)
	defer stopHeartbeat()

	var sizes []models.ImageProcessingRequestSize
	if err := facades.Orm().Query().Where("image_processing_request_id", request.ID).Find(&sizes); err != nil {
		return fmt.Errorf("load requested sizes: %w", err)
	}
	if len(sizes) == 0 {
		return permanent(fmt.Errorf("request %d has no requested sizes", request.ID))
	}

	ctx := context.Background()

	sourcePath, cleanupSource, err := r.resolveSource(&request)
	if err != nil {
		// A missing/invalid stored upload is never something a retry
		// fixes.
		return r.fail(&request, permanent(err))
	}
	defer cleanupSource()

	processor := imageprocessing.NewGovipsImageProcessor()

	info, err := processor.Inspect(ctx, sourcePath)
	if err != nil {
		return r.fail(&request, permanent(fmt.Errorf("invalid image: %w", err)))
	}

	sourceWidth, sourceHeight := info.Width, info.Height
	if err := updateRequest(request.ID, map[string]any{
		"source_width":  &sourceWidth,
		"source_height": &sourceHeight,
	}); err != nil {
		return fmt.Errorf("record source dimensions: %w", err)
	}

	if info.Width > imageconfig.MaxImageWidth() || info.Height > imageconfig.MaxImageHeight() {
		return r.fail(&request, permanent(fmt.Errorf(
			"source image %dx%d exceeds the configured maximum of %dx%d",
			info.Width, info.Height, imageconfig.MaxImageWidth(), imageconfig.MaxImageHeight())))
	}

	// requestedByID maps a size back to what the client actually asked for
	// (used for the storage folder name below) - out.Width/out.Height can
	// differ from this for aspect-ratio-preserving modes like "contain",
	// but the folder should reflect the request, not the post-resize
	// pixel dimensions.
	requestedByID := make(map[uint][2]int, len(sizes))
	specs := make([]imageprocessing.SizeSpec, 0, len(sizes))
	for _, s := range sizes {
		requestedByID[s.ID] = [2]int{s.Width, s.Height}
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

	outputs, sizeErrs, err := processor.ProcessSizes(ctx, sourcePath, specs)
	if err != nil {
		return r.fail(&request, permanent(fmt.Errorf("processing failed: %w", err)))
	}

	for _, se := range sizeErrs {
		markSizeFailed(se.RequestSizeID, se.Err.Error())
	}

	outputHash, err := r.outputHashFor(&request)
	if err != nil {
		return r.fail(&request, fmt.Errorf("assign output hash: %w", err))
	}

	successCount := 0
	for _, out := range outputs {
		requested := requestedByID[out.RequestSizeID]
		if err := r.persistOutput(&request, out, outputHash, requested[0], requested[1]); err != nil {
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

	return updateRequest(request.ID, map[string]any{
		"status":       finalStatus,
		"completed_at": carbon.NewDateTime(carbon.Now()),
	})
}

// startHeartbeat launches a goroutine that refreshes requestID's
// last_heartbeat_at every imageconfig.HeartbeatInterval() until the returned
// stop func is called. The caller must defer stop() so the goroutine is
// always cleaned up, including on early-return error paths.
func (r *ProcessImageRequestJob) startHeartbeat(requestID uint) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		ticker := time.NewTicker(imageconfig.HeartbeatInterval())
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = updateRequest(requestID, map[string]any{"last_heartbeat_at": carbon.NewDateTime(carbon.Now())})
			case <-done:
				return
			}
		}
	}()

	return func() {
		close(done)
		wg.Wait()
	}
}

// resolveSource gets a local filesystem path to the uploaded source image,
// plus a cleanup func the caller must defer that removes its
// uploads/{id} directory once processing is done with it - uploaded
// originals aren't part of the retained/generated output set, so they don't
// need to hang around after the job finishes.
func (r *ProcessImageRequestJob) resolveSource(request *models.ImageProcessingRequest) (path string, cleanup func(), err error) {
	if request.SourceFilePath == nil || *request.SourceFilePath == "" {
		return "", func() {}, fmt.Errorf("request %d has no stored source file", request.ID)
	}
	dir := storage.UploadDir(request.ID)
	return storage.Path(*request.SourceFilePath), func() {
		_ = storage.DeleteDirectory(dir)
	}, nil
}

// outputHashFor returns this request's shared output filename (see
// storage.NewOutputHash), generating and persisting one if this is the
// first attempt. Persisting it means a later retry reuses the same value
// instead of orphaning already-written sizes under a hash no other size
// will ever reference again.
func (r *ProcessImageRequestJob) outputHashFor(request *models.ImageProcessingRequest) (string, error) {
	if request.OutputHash != nil && *request.OutputHash != "" {
		return *request.OutputHash, nil
	}
	hash, err := storage.NewOutputHash()
	if err != nil {
		return "", err
	}
	if err := updateRequest(request.ID, map[string]any{"output_hash": hash}); err != nil {
		return "", fmt.Errorf("record output hash: %w", err)
	}
	request.OutputHash = &hash
	return hash, nil
}

// persistOutput is idempotent: a retry that re-runs this size will find the
// existing ImageOutput row (unique on request_size_id) and skip re-writing
// it, rather than creating a duplicate. requestedWidth/requestedHeight are
// what the client asked for (the storage folder name); out.Width/out.Height
// are the actual, possibly-different, resulting pixel dimensions (recorded
// on the row itself, same as before).
func (r *ProcessImageRequestJob) persistOutput(request *models.ImageProcessingRequest, out imageprocessing.Output, outputHash string, requestedWidth, requestedHeight int) error {
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
		// Outputs are permanent now - no ExpiresAt, nothing cleans these up.
	}
	if err := facades.Orm().Query().Create(output); err != nil {
		return fmt.Errorf("create output row: %w", err)
	}

	path := storage.OutputPath(requestedWidth, requestedHeight, outputHash)
	if err := storage.PutOutput(path, out.Bytes); err != nil {
		// Roll back the row so a retry can cleanly recreate it rather than
		// leaving a DB row with no backing file.
		_, _ = facades.Orm().Query().Delete(output)
		return fmt.Errorf("store output file: %w", err)
	}

	if _, err := facades.Orm().Query().Model(&models.ImageOutput{}).Where("id", output.ID).
		Update("storage_path", path); err != nil {
		return fmt.Errorf("record storage path: %w", err)
	}

	markSizeCompleted(out.RequestSizeID)
	return nil
}

func (r *ProcessImageRequestJob) fail(request *models.ImageProcessingRequest, err error) error {
	msg := err.Error()
	_ = updateRequest(request.ID, map[string]any{
		"status":        models.RequestStatusFailed,
		"error_message": msg,
		"completed_at":  carbon.NewDateTime(carbon.Now()),
	})
	return err
}

// updateRequest updates image_processing_requests by id against a fresh,
// unloaded model rather than a previously-Select()'d struct (the pattern
// already used by markSizeFailed/markSizeCompleted below). Using an already
// -loaded *ImageProcessingRequest with .Model() intermittently dropped some
// map fields from the generated UPDATE in testing; scoping purely by
// Where("id", ...) against an empty model avoids whatever GORM state that
// loaded struct was carrying and reliably writes every key in the map.
func updateRequest(id uint, values map[string]any) error {
	_, err := facades.Orm().Query().Model(&models.ImageProcessingRequest{}).
		Where("id", id).
		Update(values)
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
