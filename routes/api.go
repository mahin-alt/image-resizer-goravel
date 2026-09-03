package routes

import (
	"github.com/goravel/framework/contracts/route"

	"goravel/app/facades"
	"goravel/app/http/controllers"
)

// Api registers the JSON API (v1). Generated WebP outputs are served
// directly from the "s3" disk (see config/filesystems.go, the "url" field
// in image_resource.go) - there's no backend-proxied download route.
func Api() {
	imageController := controllers.NewImageController()

	facades.Route().Prefix("api/v1").Group(func(router route.Router) {
		router.Post("images", imageController.Store)
		router.Get("images/{id}", imageController.Show)
	})
}
