package models

import (
	"github.com/goravel/framework/database/orm"
	"github.com/goravel/framework/support/carbon"
)

// ImageOutput is one successfully generated WebP file. The binary itself
// lives on disk (see app/storage) at StoragePath; only metadata and the path
// are persisted here. ExpiresAt is computed once, at creation time, from the
// retention period configured at that moment - it is never recomputed from
// IMAGE_RETENTION_HOURS later, so changing that env var doesn't change the
// lifetime of images that already exist.
type ImageOutput struct {
	orm.Model
	ImageProcessingRequestID     uint             `gorm:"column:image_processing_request_id;not null;index" json:"-"`
	ImageProcessingRequestSizeID uint             `gorm:"column:image_processing_request_size_id;not null;uniqueIndex" json:"-"`
	Width                        int              `gorm:"column:width;not null" json:"width"`
	Height                       int              `gorm:"column:height;not null" json:"height"`
	Mode                         string           `gorm:"column:mode;type:varchar(16);not null" json:"mode"`
	Format                       string           `gorm:"column:format;type:varchar(8);not null" json:"format"`
	StoragePath                  string           `gorm:"column:storage_path;type:text;not null" json:"-"`
	FileSize                     int64            `gorm:"column:file_size;not null" json:"file_size"`
	ExpiresAt                    *carbon.DateTime `gorm:"column:expires_at;not null;index" json:"expires_at"`
}

func (r *ImageOutput) TableName() string {
	return "image_outputs"
}
