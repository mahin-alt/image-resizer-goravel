package models

import (
	"github.com/goravel/framework/database/orm"
	"github.com/goravel/framework/support/carbon"
)

// Status values for ImageProcessingRequest.Status. A request moves
// pending -> processing -> {completed, partially_completed, failed}.
// "expired" is set by the cleanup job once every output has been removed.
const (
	RequestStatusPending            = "pending"
	RequestStatusProcessing         = "processing"
	RequestStatusCompleted          = "completed"
	RequestStatusPartiallyCompleted = "partially_completed"
	RequestStatusFailed             = "failed"
	RequestStatusExpired            = "expired"
)

// InputType values for ImageProcessingRequest.InputType - which of the two
// supported ways the source image was supplied.
const (
	InputTypeURL    = "url"
	InputTypeUpload = "upload"
)

// ImageProcessingRequest represents one image-processing request: a source
// image (supplied either as a URL or as a local file upload) plus the set
// of sizes requested for it. It owns many ImageProcessingRequestSize rows
// (one per requested size) and, once processed, ImageOutput rows (one per
// size that succeeded).
type ImageProcessingRequest struct {
	orm.Model
	// SourceURL holds the client-supplied image_url for InputTypeURL
	// requests, and is empty for InputTypeUpload requests.
	SourceURL string `gorm:"column:source_url;type:text;not null" json:"source_url,omitempty"`
	// InputType records which of the two supported input methods this
	// request used - see the InputType* constants above.
	InputType string `gorm:"column:input_type;type:varchar(16);not null;default:url" json:"input_type"`
	// SourceFilePath is the storage path (on the "images" disk, under
	// uploads/) of an uploaded original, for InputTypeUpload requests only.
	// The worker deletes this file once processing finishes (success or
	// failure) - it isn't part of the retained/generated output set, so it
	// isn't nil-checked/exposed in API responses.
	SourceFilePath *string          `gorm:"column:source_file_path;type:text" json:"-"`
	Status         string           `gorm:"column:status;type:varchar(32);not null;index" json:"status"`
	ErrorMessage   *string          `gorm:"column:error_message;type:text" json:"error_message,omitempty"`
	StartedAt      *carbon.DateTime `gorm:"column:started_at" json:"started_at,omitempty"`
	CompletedAt    *carbon.DateTime `gorm:"column:completed_at" json:"completed_at,omitempty"`
	// SourceWidth/SourceHeight are the source image's own dimensions, filled
	// in once the worker has loaded/inspected it - nil while status is
	// still pending/processing, or if the request failed before that point
	// (e.g. the download itself failed).
	SourceWidth  *int `gorm:"column:source_width" json:"-"`
	SourceHeight *int `gorm:"column:source_height" json:"-"`
	// OutputHash is the single random name shared by every output this
	// request generates (media/{width}x{height}/{output_hash}.webp) - see
	// app/storage.NewOutputHash. Generated lazily on first use and
	// persisted so a retry reuses it instead of a fresh one.
	OutputHash *string `gorm:"column:output_hash;type:varchar(64)" json:"-"`

	Sizes   []ImageProcessingRequestSize `gorm:"foreignKey:ImageProcessingRequestID" json:"-"`
	Outputs []ImageOutput                `gorm:"foreignKey:ImageProcessingRequestID" json:"-"`
}

func (r *ImageProcessingRequest) TableName() string {
	return "image_processing_requests"
}
