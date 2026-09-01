package models

import (
	"github.com/goravel/framework/database/orm"
)

// Resize mode values accepted in the API's "mode" field, and stored here so
// the job knows exactly how to reproduce each output on retry.
const (
	ResizeModeContain = "contain" // fit entirely inside the box, no crop, no stretch
	ResizeModeFit     = "fit"     // alias of contain
	ResizeModeCover   = "cover"   // fill the box, crop overflow, saliency-aware
	ResizeModeCrop    = "crop"    // fill the box, crop overflow, fixed centre crop
	ResizeModeFill    = "fill"    // exact width x height, ignores aspect ratio (the only mode that stretches)
)

const (
	SizeStatusPending   = "pending"
	SizeStatusCompleted = "completed"
	SizeStatusFailed    = "failed"
)

// ImageProcessingRequestSize is one requested output size within a request.
// It exists as its own row (rather than JSON on the parent) so a failure on
// one size can be recorded independently of the others, and so ImageOutput
// has a stable row to point back to.
type ImageProcessingRequestSize struct {
	orm.Model
	ImageProcessingRequestID uint    `gorm:"column:image_processing_request_id;not null;index" json:"-"`
	Width                    int     `gorm:"column:width;not null" json:"width"`
	Height                   int     `gorm:"column:height;not null" json:"height"`
	Mode                     string  `gorm:"column:mode;type:varchar(16);not null" json:"mode"`
	AllowUpscale             bool    `gorm:"column:allow_upscale;not null" json:"allow_upscale"`
	Quality                  int     `gorm:"column:quality;not null" json:"quality"`
	Status                   string  `gorm:"column:status;type:varchar(16);not null" json:"status"`
	ErrorMessage             *string `gorm:"column:error_message;type:text" json:"error_message,omitempty"`
}

func (r *ImageProcessingRequestSize) TableName() string {
	return "image_processing_request_sizes"
}
