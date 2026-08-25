package mcp

import (
	"encoding/json"
	"fmt"
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
