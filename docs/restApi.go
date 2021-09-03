package docs

import "github.com/gatsby-tv/dapper/api"

// swagger:route GET /status foobar-tag idOfFoobarEndpoint
// Retrieve the status of a currently encoding video.
// responses:
//   200: success

// This text will appear as description of your response body.
// swagger:response success
type videoEncodingStatusResponseWrapper struct {
	// in:body
	Body api.VideoEncodingStatusResponse
}

// // swagger:parameters idOfFoobarEndpoint
// type foobarParamsWrapper struct {
// 	// This text will appear as description of your request body.
// 	// in:body
// 	Body api.FooBarRequest
// }
