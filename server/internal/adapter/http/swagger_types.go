package http

// ErrorResponseBody represents an error response
// @Description Error response body
type ErrorResponseBody struct {
	Error   string `json:"error" example:"error_code"`
	Message string `json:"message,omitempty" example:"Human readable message"`
}

// SuccessResponseBody represents a success response
// @Description Success response body
type SuccessResponseBody struct {
	Data interface{} `json:"data"`
}
