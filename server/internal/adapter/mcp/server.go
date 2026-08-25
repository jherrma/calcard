package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	addressbookuc "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
	contactuc "github.com/jherrma/caldav-server/internal/usecase/contact"
	eventuc "github.com/jherrma/caldav-server/internal/usecase/event"
	searchuc "github.com/jherrma/caldav-server/internal/usecase/search"
)

// Deps are the use cases and repositories the tools delegate to.
//
// They are the SAME instances the REST handlers get, wired in routes.go. That
// is the whole design: MCP adds no persistence, no queries and no permission
// logic of its own, so a fix to a use case fixes both surfaces at once and the
// two can never disagree about what a user is allowed to see.
type Deps struct {
	CalendarRepo    calendar.CalendarRepository
	AddressBookRepo addressbook.Repository

	CalendarList *calendaruc.ListCalendarsUseCase

	EventList   *eventuc.ListEventsUseCase
	EventGet    *eventuc.GetEventUseCase
	EventCreate *eventuc.CreateEventUseCase
	EventUpdate *eventuc.UpdateEventUseCase
	EventDelete *eventuc.DeleteEventUseCase

	AddressBookList *addressbookuc.ListUseCase

	ContactList   *contactuc.ListUseCase
	ContactGet    *contactuc.GetUseCase
	ContactCreate *contactuc.CreateUseCase
	ContactUpdate *contactuc.UpdateUseCase
	ContactDelete *contactuc.DeleteUseCase

	Search *searchuc.UseCase
}

// toolFunc executes one tool.
//
// The two return values separate the two kinds of failure MCP distinguishes: a
// *toolCallResult with IsError set is a tool that ran and failed in a way the
// model should read and react to ("that calendar is read-only"), while an
// *RPCError is a protocol fault the model cannot act on (bad JSON, unknown
// tool). Conflating them either hides real errors from the model or turns
// ordinary refusals into transport errors.
type toolFunc func(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError)

// Server dispatches MCP JSON-RPC messages. It is stateless apart from the
// session store and is safe for concurrent use.
type Server struct {
	deps     Deps
	sessions *SessionStore
	tools    map[string]toolFunc
	toolList []Tool
	// now is the clock the tools see. Injectable so scheduling behaviour is
	// testable without sleeping.
	now func() time.Time
}

// NewServer builds the dispatcher and registers every tool.
func NewServer(deps Deps) *Server {
	s := &Server{
		deps:     deps,
		sessions: NewSessionStore(),
		tools:    make(map[string]toolFunc),
		now:      time.Now,
	}
	s.registerCalendarTools()
	s.registerContactTools()
	s.registerSchedulingTools()
	s.buildToolList()
	return s
}

// Sessions exposes the session store to the transport.
func (s *Server) Sessions() *SessionStore { return s.sessions }

// register adds one tool to the registry. A duplicate name is a programming
// error and panics at construction rather than silently shadowing.
func (s *Server) register(tool Tool, fn toolFunc) {
	if _, exists := s.tools[tool.Name]; exists {
		panic("mcp: duplicate tool name " + tool.Name)
	}
	s.tools[tool.Name] = fn
	s.toolList = append(s.toolList, tool)
}

// buildToolList sorts the advertised tools by name so tools/list is stable
// across restarts — an unstable order shows up as spurious diffs in client
// caches and in test fixtures.
func (s *Server) buildToolList() {
	sort.Slice(s.toolList, func(i, j int) bool { return s.toolList[i].Name < s.toolList[j].Name })
}

// Handle dispatches one JSON-RPC request for an authenticated user.
//
// The first return value is nil for notifications, which by JSON-RPC MUST NOT
// be answered — the transport turns that into an empty 202 rather than a body.
// The second is a freshly opened session id, non-empty only for `initialize`;
// it is returned rather than written into the response because it travels as
// the Mcp-Session-Id HTTP header, not in the JSON body.
func (s *Server) Handle(ctx context.Context, userID uint, req *Request) (*Response, string) {
	if req.JSONRPC != jsonRPCVersion {
		if req.IsNotification() {
			return nil, ""
		}
		return errorResponse(req.ID, rpcErrorf(CodeInvalidRequest, "unsupported jsonrpc version %q, expected %q", req.JSONRPC, jsonRPCVersion)), ""
	}
	if req.Method == "" {
		if req.IsNotification() {
			return nil, ""
		}
		return errorResponse(req.ID, rpcError(CodeInvalidRequest, "missing method")), ""
	}

	// Notifications are acknowledged by doing nothing.
	// notifications/initialized is the only one a client sends us today;
	// unknown ones are ignored rather than rejected, as the spec requires.
	if req.IsNotification() {
		return nil, ""
	}

	cc := &callContext{ctx: ctx, userID: userID, now: s.now()}

	switch req.Method {
	case MethodInitialize:
		return s.handleInitialize(req, userID)
	case MethodPing:
		// Ping's result is an empty object, not null: some clients treat a null
		// result as a malformed response.
		return resultResponse(req.ID, struct{}{}), ""
	case MethodToolsList:
		return resultResponse(req.ID, toolsListResult{Tools: s.toolList}), ""
	case MethodToolsCall:
		return s.handleToolCall(cc, req), ""
	case MethodResourcesList:
		return resultResponse(req.ID, resourcesListResult{Resources: s.listResources(cc)}), ""
	case MethodResourcesTemplateList:
		return resultResponse(req.ID, resourceTemplatesListResult{ResourceTemplates: resourceTemplates()}), ""
	case MethodResourcesRead:
		return s.handleResourceRead(cc, req), ""
	default:
		return errorResponse(req.ID, rpcErrorf(CodeMethodNotFound, "unknown method %q", req.Method)), ""
	}
}

// handleInitialize performs the handshake and opens a session.
func (s *Server) handleInitialize(req *Request, userID uint) (*Response, string) {
	var params initializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errorResponse(req.ID, rpcErrorf(CodeInvalidParams, "invalid initialize params: %v", err)), ""
		}
	}

	clientName := ""
	if params.ClientInfo != nil {
		clientName = params.ClientInfo.Name
	}
	version := negotiateProtocolVersion(params.ProtocolVersion)

	sess, err := s.sessions.Create(userID, version, clientName)
	if err != nil {
		return errorResponse(req.ID, rpcError(CodeInternalError, "failed to open session")), ""
	}

	return resultResponse(req.ID, initializeResult{
		ProtocolVersion: version,
		ServerInfo:      serverInfo{Name: ServerName, Version: ServerVersion},
		Capabilities: capabilities{
			Tools:     &toolsCapability{},
			Resources: &resourcesCapability{},
		},
		Instructions: instructions,
	}), sess.ID
}

// handleToolCall routes tools/call to the registered implementation.
func (s *Server) handleToolCall(cc *callContext, req *Request) *Response {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, rpcErrorf(CodeInvalidParams, "invalid tools/call params: %v", err))
	}
	fn, ok := s.tools[params.Name]
	if !ok {
		return errorResponse(req.ID, rpcErrorf(CodeMethodNotFound, "unknown tool %q", params.Name))
	}

	args := params.Arguments
	if len(args) == 0 {
		// A tool with no required arguments may be called with `arguments`
		// omitted entirely; give the decoder an empty object rather than
		// failing on empty input.
		args = json.RawMessage(`{}`)
	}

	result, rpcErr := fn(cc, args)
	if rpcErr != nil {
		return errorResponse(req.ID, rpcErr)
	}
	return resultResponse(req.ID, result)
}

// jsonText renders a value as the pretty JSON that becomes a tool's text
// content.
//
// Tools answer with JSON rather than the prose the story sketched, because
// prose drops the identifiers: "Event 'Team Standup' created successfully"
// leaves the model with no id to update or delete it by, forcing a re-list
// after every write. JSON keeps ids, permissions and timestamps addressable.
func jsonText(v interface{}) (*toolCallResult, *RPCError) {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, rpcErrorf(CodeInternalError, "failed to encode result: %v", err)
	}
	return textResult(string(buf)), nil
}

// decodeArgs unmarshals tool arguments, reporting a schema mismatch as an
// invalid-params protocol error (the client sent something its own schema
// forbade) rather than a tool error.
func decodeArgs(args json.RawMessage, dst interface{}) *RPCError {
	if err := json.Unmarshal(args, dst); err != nil {
		return rpcErrorf(CodeInvalidParams, "invalid arguments: %v", err)
	}
	return nil
}

// parseTime accepts an RFC 3339 timestamp, and also a bare date for the
// convenience of all-day handling. field names the argument in the error so a
// model can fix the right one.
func parseTime(field, value string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%s must be an RFC 3339 timestamp (e.g. 2026-03-06T09:00:00Z) or a YYYY-MM-DD date, got %q", field, value)
}
