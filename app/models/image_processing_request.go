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

// ImageProcessingRequest represents one POST /api/v1/images call: a source
// URL plus the set of sizes requested for it. It owns many
// ImageProcessingRequestSize rows (one per requested size) and, once
// processed, ImageOutput rows (one per size that succeeded).
type ImageProcessingRequest struct {
	orm.Model
	SourceURL    string           `gorm:"column:source_url;type:text;not null" json:"source_url"`
	Status       string           `gorm:"column:status;type:varchar(32);not null;index" json:"status"`
	ErrorMessage *string          `gorm:"column:error_message;type:text" json:"error_message,omitempty"`
	StartedAt    *carbon.DateTime `gorm:"column:started_at" json:"started_at,omitempty"`
	CompletedAt  *carbon.DateTime `gorm:"column:completed_at" json:"completed_at,omitempty"`

	Sizes   []ImageProcessingRequestSize `gorm:"foreignKey:ImageProcessingRequestID" json:"-"`
	Outputs []ImageOutput                `gorm:"foreignKey:ImageProcessingRequestID" json:"-"`
}

func (r *ImageProcessingRequest) TableName() string {
	return "image_processing_requests"
}
