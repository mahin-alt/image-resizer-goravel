package routes

import (
	"github.com/goravel/framework/contracts/route"

	"goravel/app/facades"
	"goravel/app/http/controllers"
	"goravel/app/support/imageconfig"
)

// Api registers the JSON API (v1) plus a static route serving generated
// WebP files from the "images" disk (see config/filesystems.go).
func Api() {
	imageController := controllers.NewImageController()

	facades.Route().Prefix("api/v1").Group(func(router route.Router) {
		router.Post("images", imageController.Store)
		router.Get("images/{id}", imageController.Show)
	})

	facades.Route().Static("images", imageconfig.StoragePath())
}
