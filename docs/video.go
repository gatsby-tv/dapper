package docs

import (
	"mime/multipart"

	"github.com/gatsby-tv/dapper/api"
)

// swagger:route POST /video videoUpload-tag videoUpload
// Upload a new video to dapper.
// responses:
//   200: success
//   400: badRequest
//   500: processingError

// swagger:parameters videoUpload
type videoUploadParamsWrapper struct {
	// Video file to upload
	// in:form
	Video multipart.File `json:"video"`
}

// Video has been queued for upload and is accessible with the given ID.
// swagger:response success
type videoUploadSuccessResponseWrapper struct {
	// in:body
	Body api.VideoStartEncodingResponse
}

// Submitted form contains invalid or missing data.
// swagger:response badRequest
type videoUploadBadRequestResponseWrapper struct {
	// in:body
	Error string
}

// An internal error occurred while trying to queue the video for encoding.
// swagger:response processingError
type videoUploadProcessingErrorResponseWrapper struct {
	// in:body
	Error string
}
