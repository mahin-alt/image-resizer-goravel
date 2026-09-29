package routes

import (
	"github.com/goravel/framework/contracts/route"

	"goravel/app/facades"
	"goravel/app/http/controllers"
)

// Api registers the JSON API (v1). Generated WebP outputs are served
// directly from the "s3" disk (see config/filesystems.go, the "url" field
// in image_resource.go) - there's no backend-proxied download route.
//
// POST images and POST images/{id}/retry are both synchronous: they block
// until processing finishes and return the final result directly (see
// controllers.ImageController.Store) - there's no separate status/polling
// endpoint. images/{id} (Show) still exists for looking a request back up
// by id afterward.
func Api() {
	imageController := controllers.NewImageController()

	facades.Route().Prefix("api/v1").Group(func(router route.Router) {
		router.Post("images", imageController.Store)
		router.Get("images/{id}", imageController.Show)
		router.Post("images/{id}/retry", imageController.Retry)
	})
}
