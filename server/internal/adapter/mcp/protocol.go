// Package mcp implements the Model Context Protocol server (story 104): a
// JSON-RPC 2.0 tool/resource surface over Streamable HTTP that lets an AI
// assistant read and manage the caller's calendars and contacts.
//
// The package is a transport and a facade, nothing more. Every tool delegates
// to the same use cases the REST handlers call, and every permission decision
// goes through the same repository methods (GetUserPermission for calendars,
// the address-book equivalent for contacts), so an MCP client can never reach
// data the REST API would refuse it. There is no MCP-specific business logic
// and no MCP-specific query — that is the property to preserve when adding
// tools.
package mcp

import "encoding/json"

// JSON-RPC 2.0 wire version. Any other value in a request is rejected.
const jsonRPCVersion = "2.0"

// Request is one inbound JSON-RPC message.
//
// ID is kept as raw JSON because JSON-RPC allows a string or a number and the
// response must echo it back byte-identically; decoding it into a Go type and
// re-encoding would turn 1 into 1.0 for some clients. A nil ID means the
// message is a notification and MUST NOT be answered.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification reports whether the message must go unanswered.
//
// A literal `"id": null` is a malformed request rather than a notification, but
// treating it as one is the safer reading: answering with a null id is useless
// to the client either way.
func (r Request) IsNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

// Response is one outbound JSON-RPC message. Exactly one of Result and Error
// is populated.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is the JSON-RPC error object.
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return e.Message }

// MCP method names.
const (
	MethodInitialize            = "initialize"
	MethodInitialized           = "notifications/initialized"
	MethodPing                  = "ping"
	MethodToolsList             = "tools/list"
	MethodToolsCall             = "tools/call"
	MethodResourcesList         = "resources/list"
	MethodResourcesTemplateList = "resources/templates/list"
	MethodResourcesRead         = "resources/read"
)

// Protocol versions this server speaks, newest first.
//
// A client names the version it wants in `initialize`; if we know it we echo it
// back, otherwise we answer with PreferredProtocolVersion and let the client
// decide whether it can live with that (which is what the spec prescribes —
// disconnecting on an unknown version would break every client released after
// this code was written).
const PreferredProtocolVersion = "2025-06-18"

var supportedProtocolVersions = []string{
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

// negotiateProtocolVersion resolves the version to report in the initialize
// result.
func negotiateProtocolVersion(requested string) string {
	for _, v := range supportedProtocolVersions {
		if v == requested {
			return v
		}
	}
	return PreferredProtocolVersion
}

// ServerName and ServerVersion identify this implementation to clients.
const (
	ServerName    = "calcard"
	ServerVersion = "1.0.0"
)

// initializeParams is the subset of the handshake we act on.
type initializeParams struct {
	ProtocolVersion string      `json:"protocolVersion"`
	ClientInfo      *clientInfo `json:"clientInfo,omitempty"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// capabilities advertises what the server offers. Both tools and resources are
// static: nothing here changes at runtime, so neither declares listChanged.
type capabilities struct {
	Tools     *toolsCapability     `json:"tools,omitempty"`
	Resources *resourcesCapability `json:"resources,omitempty"`
}

type toolsCapability struct{}

// resourcesCapability leaves subscribe unset: the server has no change feed to
// subscribe to yet, and advertising one it cannot honour is worse than not
// offering it.
type resourcesCapability struct{}

type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	ServerInfo      serverInfo   `json:"serverInfo"`
	Capabilities    capabilities `json:"capabilities"`
	Instructions    string       `json:"instructions,omitempty"`
}

// instructions is prose handed to the model at handshake time. It is the one
// place to state conventions that no per-tool schema can carry.
const instructions = `CalCard exposes the signed-in user's calendars and contacts.

Identifiers are UUIDs, not numbers: pass the ` + "`id`" + ` returned by list_calendars,
get_events, list_address_books or get_contacts, never a positional index.

Times are RFC 3339. A time without an offset is ambiguous — send an offset (or a
trailing Z) so the event lands where the user meant it. All-day events are
expressed with all_day: true and date-only semantics.

Calendars and address books shared with the user are included in every listing
and are marked with their permission; a read-only collection will refuse writes.
Prefer search_events / search_contacts over listing everything and filtering.`

// Tool is one AI-callable function, as advertised by tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

// toolCallParams is the tools/call request payload.
type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// Content is one block of tool output. Only text blocks are produced here.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toolCallResult is what tools/call returns.
//
// IsError is how MCP reports a *tool* failure, as opposed to a protocol
// failure: the call itself succeeded, so it is a JSON-RPC result rather than a
// JSON-RPC error, and the model gets to read the message and try something
// else. Protocol-level problems (unknown tool, unparseable arguments) stay
// JSON-RPC errors.
type toolCallResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

func textResult(text string) *toolCallResult {
	return &toolCallResult{Content: []Content{{Type: "text", Text: text}}}
}

func errorResult(text string) *toolCallResult {
	return &toolCallResult{Content: []Content{{Type: "text", Text: text}}, IsError: true}
}

// Resource is one AI-readable document.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
}

// ResourceTemplate describes a family of resources addressed by a URI template
// (RFC 6570), e.g. every calendar's event list.
type ResourceTemplate struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
}

type resourcesListResult struct {
	Resources []Resource `json:"resources"`
}

type resourceTemplatesListResult struct {
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates"`
}

type resourceReadParams struct {
	URI string `json:"uri"`
}

// ResourceContents is one resource body. Everything this server exposes is
// JSON, so only Text is ever populated.
type ResourceContents struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType"`
	Text     string `json:"text"`
}

type resourcesReadResult struct {
	Contents []ResourceContents `json:"contents"`
}
