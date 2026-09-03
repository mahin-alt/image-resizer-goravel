package routes

import (
	"github.com/goravel/framework/contracts/route"

	"goravel/app/facades"
	"goravel/app/http/controllers"
)

// Api registers the JSON API (v1). Generated WebP outputs are served from
// the "s3" disk (see config/filesystems.go) - there's no local static route
// for them.
func Api() {
	imageController := controllers.NewImageController()

	facades.Route().Prefix("api/v1").Group(func(router route.Router) {
		router.Post("images", imageController.Store)
		router.Get("images/{id}", imageController.Show)
		router.Get("images/{id}/outputs/{outputId}/download", imageController.Download)
	})
}
