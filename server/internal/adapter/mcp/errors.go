package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// JSON-RPC 2.0 reserved error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Implementation-defined codes live in the -32000..-32099 band the spec
// reserves for the server.
const (
	// CodeResourceNotFound is the code MCP defines for resources/read against a
	// URI the server does not serve.
	CodeResourceNotFound = -32002
)

func rpcError(code int, message string) *RPCError {
	return &RPCError{Code: code, Message: message}
}

func rpcErrorf(code int, format string, args ...interface{}) *RPCError {
	return &RPCError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// errorResponse builds a JSON-RPC error response for the given id.
func errorResponse(id json.RawMessage, err *RPCError) *Response {
	if id == nil {
		id = json.RawMessage("null")
	}
	return &Response{JSONRPC: jsonRPCVersion, ID: id, Error: err}
}

// resultResponse builds a JSON-RPC success response for the given id.
func resultResponse(id json.RawMessage, result interface{}) *Response {
	if id == nil {
		id = json.RawMessage("null")
	}
	return &Response{JSONRPC: jsonRPCVersion, ID: id, Result: result}
}

// Internal errors.
//
// A tool or resource that fails for a reason the caller cannot fix — storage,
// typically — answers with ONE generic sentence and logs the cause. Same rule
// as REST's calendarError (F28): a driver message ("no such table: …",
// constraint names, SQL, file paths) describes the server's internals, not the
// request, and everything a tool returns lands in the model's context. Only the
// errors a caller can act on keep their own text: validation (the use cases'
// typed ErrInvalidInput and friends), a refused permission and "not found".
// Everything else goes through toolInternalError or resourceInternalError —
// never `"Failed to …: " + err.Error()`.

// internalErrorText is the sentence the caller sees. what names the operation
// that failed: the tool name, or the JSON-RPC method for a resource.
func internalErrorText(what string) string {
	return "An internal server error stopped " + what + ". The details were logged on the server; try again later."
}

// logInternalError records the cause the caller is not shown. attrs locate the
// failure (tool, uri, calendar); the error itself is always the last attribute.
func logInternalError(msg string, err error, attrs ...slog.Attr) {
	args := make([]any, 0, len(attrs)+1)
	for _, a := range attrs {
		args = append(args, a)
	}
	args = append(args, slog.Any("error", err))
	slog.Error(msg, args...)
}

// toolInternalError answers a tool whose use case failed for a reason the
// caller cannot fix: a generic tool error for the model, the cause in the log.
func toolInternalError(tool string, err error) *toolCallResult {
	logInternalError("mcp tool failed", err, slog.String("tool", tool))
	return errorResult(internalErrorText(tool))
}

// resourceInternalError is toolInternalError for resources/read, which reports
// failures as JSON-RPC errors rather than tool results.
func resourceInternalError(uri string, err error) *RPCError {
	logInternalError("mcp resource read failed", err, slog.String("uri", uri))
	return rpcError(CodeInternalError, internalErrorText(MethodResourcesRead))
}
