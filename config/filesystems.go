package config

import (
	"github.com/goravel/framework/contracts/filesystem"
	"github.com/goravel/framework/support/path"

	s3facades "github.com/goravel/s3/facades"

	"goravel/app/facades"
)

func init() {
	config := facades.Config()
	config.Add("filesystems", map[string]any{
		// Default Filesystem Disk
		//
		// Here you may specify the default filesystem disk that should be used
		// by the framework. The "local" disk, as well as a variety of cloud
		// based disks are available to your application. Just store away!
		"default": "local",

		// Filesystem Disks
		//
		// Here you may configure as many filesystem "disks" as you wish, and you
		// may even configure multiple disks of the same driver. Defaults have
		// been set up for each driver as an example of the required values.
		//
		// Supported Drivers: "local", "custom"
		"disks": map[string]any{
			"local": map[string]any{
				"driver": "local",
				"root":   path.Storage("app"),
			},
			"public": map[string]any{
				"driver": "local",
				"root":   path.Storage("app/public"),
				"url":    config.Env("APP_URL", "").(string) + "/storage",
			},
			// Generated WebP outputs. Root is configurable via STORAGE_PATH
			// (see config/image.go) rather than being read anywhere else in
			// the app - keep it in sync with the same env var name.
			"images": map[string]any{
				"driver": "local",
				"root":   config.Env("STORAGE_PATH", "storage/app/images"),
				"url":    config.Env("APP_URL", "").(string) + "/images",
			},
			// Generated WebP outputs live here permanently - an S3-compatible
			// bucket (MinIO in dev, see docker-compose.yml/README; a real
			// MinIO/S3 endpoint in prod). See app/storage/image_storage.go's
			// Output* functions, which are the only callers that touch this
			// disk.
			"s3": map[string]any{
				"driver":            "custom",
				"key":               config.Env("AWS_ACCESS_KEY_ID", ""),
				"secret":            config.Env("AWS_SECRET_ACCESS_KEY", ""),
				"region":            config.Env("AWS_DEFAULT_REGION", ""),
				"bucket":            config.Env("AWS_BUCKET", ""),
				"url":               config.Env("AWS_URL", ""),
				"endpoint":          config.Env("AWS_ENDPOINT", ""),
				"use_path_style":    config.Env("AWS_USE_PATH_STYLE_ENDPOINT", true),
				"object_canned_acl": "public-read",
				"via": func() (filesystem.Driver, error) {
					return s3facades.S3("s3")
				},
			},
		},
	})
}
