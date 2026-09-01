package requests

// CreateImageRequestBody is the JSON body for POST /api/v1/images.
type CreateImageRequestBody struct {
	ImageURL string          `json:"image_url" form:"image_url"`
	Sizes    []RequestedSize `json:"sizes" form:"sizes"`
}

type RequestedSize struct {
	Width        int    `json:"width" form:"width"`
	Height       int    `json:"height" form:"height"`
	Mode         string `json:"mode" form:"mode"`
	AllowUpscale *bool  `json:"allow_upscale" form:"allow_upscale"`
	Quality      *int   `json:"quality" form:"quality"`
}
