package docs

import (
	"github.com/gatsby-tv/dapper/api"
)

// swagger:route GET /status encodingStatus-tag encodingStatus
// Retrieve the status of a currently encoding video.
// responses:
//   200: success
//   500: processingError

// swagger:parameters encodingStatus
type videoEncodingStatusParamsWrapper struct {
	// ID of the video to get the status of.
	// in:query
	ID string `json:"id"`
}

// Contents of the body will depend on the current step of video transcoding.
// swagger:response success
type videoEncodingStatusSuccessResponseWrapper struct {
	// in:body
	Body api.VideoEncodingStatusResponse
}

// swagger:response processingError
type videoEncodingStatusProcessingErrorResponseWrapper struct {
	// in:body
	Body api.VideoEncodingStatusResponse
}
