package requests

// CreateImageRequestBody is the "data" field's JSON shape for
// POST /api/v1/images (multipart/form-data - see the controller).
type CreateImageRequestBody struct {
	Sizes []RequestedSize `json:"sizes" form:"sizes"`
}

type RequestedSize struct {
	Width        int    `json:"width" form:"width"`
	Height       int    `json:"height" form:"height"`
	Mode         string `json:"mode" form:"mode"`
	AllowUpscale *bool  `json:"allow_upscale" form:"allow_upscale"`
	Quality      *int   `json:"quality" form:"quality"`
}
