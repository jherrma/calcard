package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
	"github.com/jherrma/caldav-server/internal/usecase/mcptoken"
)

// HTTP headers the Streamable HTTP transport defines.
const (
	headerSessionID       = "Mcp-Session-Id"
	headerProtocolVersion = "MCP-Protocol-Version"
)

// sseKeepAlive is how often the notification stream emits a comment frame.
// Idle proxies close silent connections; a periodic comment keeps the stream
// open without inventing protocol traffic.
const sseKeepAlive = 25 * time.Second

// defaultStreamLifetime bounds one GET stream when the caller gives no bound.
// The client reconnects transparently, and without a bound a half-open
// connection that never signals EOF would pin a goroutine for the life of the
// process.
const defaultStreamLifetime = 30 * time.Minute

// streamLifetimeMargin is subtracted from the server's write timeout so the
// stream closes cleanly just BEFORE fasthttp aborts it mid-write. A clean close
// is a normal end-of-stream to the client; an aborted write is an error it has
// to recover from.
const streamLifetimeMargin = 2 * time.Second

// Handler is the Streamable HTTP transport for the MCP server.
type Handler struct {
	server   *Server
	authUC   *mcptoken.AuthenticateUseCase
	jwt      user.TokenProvider
	userRepo user.UserRepository
	logger   *logging.SecurityLogger
	// streamLifetime is how long a GET notification stream is held open. It is
	// derived from the server's write timeout rather than chosen freely,
	// because fasthttp applies that timeout to the whole streamed response.
	streamLifetime time.Duration
}

func NewHandler(
	server *Server,
	authUC *mcptoken.AuthenticateUseCase,
	jwt user.TokenProvider,
	userRepo user.UserRepository,
	logger *logging.SecurityLogger,
	writeTimeout time.Duration,
) *Handler {
	return &Handler{
		server:         server,
		authUC:         authUC,
		jwt:            jwt,
		userRepo:       userRepo,
		logger:         logger,
		streamLifetime: streamLifetime(writeTimeout),
	}
}

// streamLifetime picks how long to hold a stream open given the server's write
// timeout. A zero or negative timeout means fasthttp will not abort the write,
// so the default bound applies.
func streamLifetime(writeTimeout time.Duration) time.Duration {
	if writeTimeout <= 0 {
		return defaultStreamLifetime
	}
	if writeTimeout <= streamLifetimeMargin {
		// A write timeout this short cannot carry a stream at all; hold it for
		// a moment so the client sees a well-formed, immediately-ended stream
		// rather than a truncated one.
		return writeTimeout / 2
	}
	life := writeTimeout - streamLifetimeMargin
	if life > defaultStreamLifetime {
		return defaultStreamLifetime
	}
	return life
}

// Authenticate is the middleware guarding every /mcp route.
//
// Two credentials are accepted on the same Bearer header. An MCP token is the
// intended one: long-lived, revocable, minted for exactly this endpoint. A JWT
// access token also works, which is what story 104 specified and what makes the
// endpoint usable from a browser session or a quick curl — but it expires in
// minutes, so it is not what a configured client should carry.
//
// The two are told apart by prefix, not by trying both: an MCP token is never a
// valid JWT and vice versa, and attempting the wrong verifier only produces
// misleading log noise.
func (h *Handler) Authenticate() fiber.Handler {
	return func(c fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return h.unauthorized(c, "missing authentication token", "missing_credential")
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			return h.unauthorized(c, "invalid authentication header format", "malformed_header")
		}
		credential := strings.TrimSpace(parts[1])

		if mcptoken.LooksLikeToken(credential) {
			u, _, err := h.authUC.Execute(c.Context(), credential, c.IP())
			if err != nil {
				return h.unauthorized(c, "invalid or expired MCP token", authFailureReason(err))
			}
			c.Locals("user", u)
			c.Locals("user_id", u.ID)
			c.Locals("user_uuid", u.UUID)
			return c.Next()
		}

		userUUID, email, err := h.jwt.ValidateAccessToken(credential)
		if err != nil {
			if errors.Is(err, authadapter.ErrExpiredToken) {
				return h.unauthorized(c, "token expired", "jwt_expired")
			}
			return h.unauthorized(c, "invalid or expired token", "jwt_invalid")
		}
		u, err := h.userRepo.GetByUUID(c.Context(), userUUID)
		if err != nil || u == nil {
			return h.unauthorized(c, "user not found", "jwt_user_missing")
		}
		if !u.IsActive {
			return h.unauthorized(c, "account is not active", "jwt_user_inactive")
		}
		c.Locals("user", u)
		c.Locals("user_id", u.ID)
		c.Locals("user_uuid", u.UUID)
		c.Locals("user_email", email)
		return c.Next()
	}
}

// authFailureReason maps an authentication error to the label recorded in the
// security log. The client is never told which of these applied.
func authFailureReason(err error) string {
	switch {
	case errors.Is(err, mcptoken.ErrTokenRevoked):
		return "token_revoked"
	case errors.Is(err, mcptoken.ErrTokenExpired):
		return "token_expired"
	case errors.Is(err, mcptoken.ErrUserInactive):
		return "user_inactive"
	case errors.Is(err, mcptoken.ErrTokenNotFound):
		return "token_unknown"
	default:
		return "lookup_failed"
	}
}

// unauthorized answers a rejected credential.
//
// WWW-Authenticate is set because MCP clients use it to discover that the
// endpoint wants a bearer token at all; without it a client cannot tell an
// auth failure from a broken endpoint.
func (h *Handler) unauthorized(c fiber.Ctx, message, reason string) error {
	if h.logger != nil {
		h.logger.LogMCPAuthFailure(c.Context(), reason, c.IP(), c.Get("User-Agent"))
	}
	c.Set("WWW-Authenticate", `Bearer realm="calcard-mcp"`)
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"error":   "unauthorized",
		"message": message,
	})
}

// HandleMessage serves POST /mcp — the JSON-RPC request path.
func (h *Handler) HandleMessage(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)

	sessionID := c.Get(headerSessionID)
	if sessionID != "" && h.server.Sessions().Get(sessionID, userID) == nil {
		// The spec's contract: 404 means "your session is gone, re-initialize".
		// Anything else (401, 400) makes clients give up instead of recovering.
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error":   "session_not_found",
			"message": "Unknown or expired MCP session; send initialize again.",
		})
	}

	body := c.Body()
	if len(body) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse(nil, rpcError(CodeParseError, "empty request body")))
	}

	// JSON-RPC allows a batch (a top-level array). MCP dropped batching in
	// 2025-06-18, but accepting one costs a branch and keeps older clients
	// working, so the transport handles both shapes.
	if isBatch(body) {
		return h.handleBatch(c, userID, body)
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse(nil, rpcErrorf(CodeParseError, "invalid JSON: %v", err)))
	}

	resp, newSessionID := h.server.Handle(c.Context(), userID, &req)
	if newSessionID != "" {
		c.Set(headerSessionID, newSessionID)
	}
	if resp == nil {
		return acceptedNoBody(c)
	}
	c.Set(headerProtocolVersion, PreferredProtocolVersion)
	return c.JSON(resp)
}

// acceptedNoBody answers a notification with a bodyless 202, which is what the
// spec prescribes so a client can tell "accepted, nothing to read" from an
// empty response.
//
// It cannot use c.SendStatus: that helper fills an EMPTY body with the status
// text, and 202 is not one of the statuses fasthttp forbids a body on — so the
// response would carry the six bytes "Accepted", and a client doing the obvious
// `if (res.ok) return res.json()` would blow up parsing it.
func acceptedNoBody(c fiber.Ctx) error {
	return c.Status(fiber.StatusAccepted).SendString("")
}

// handleBatch dispatches a JSON-RPC batch, dropping the responses to
// notifications as the spec requires. A batch consisting only of notifications
// gets no body at all.
func (h *Handler) handleBatch(c fiber.Ctx, userID uint, body []byte) error {
	var reqs []Request
	if err := json.Unmarshal(body, &reqs); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse(nil, rpcErrorf(CodeParseError, "invalid JSON batch: %v", err)))
	}
	if len(reqs) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse(nil, rpcError(CodeInvalidRequest, "empty batch")))
	}

	responses := make([]*Response, 0, len(reqs))
	for i := range reqs {
		resp, newSessionID := h.server.Handle(c.Context(), userID, &reqs[i])
		if newSessionID != "" {
			c.Set(headerSessionID, newSessionID)
		}
		if resp != nil {
			responses = append(responses, resp)
		}
	}
	if len(responses) == 0 {
		return acceptedNoBody(c)
	}
	c.Set(headerProtocolVersion, PreferredProtocolVersion)
	return c.JSON(responses)
}

// isBatch reports whether the body is a JSON array, ignoring leading space.
func isBatch(body []byte) bool {
	for _, b := range body {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '[':
			return true
		default:
			return false
		}
	}
	return false
}

// manifest is the discovery document served by a plain GET /mcp.
//
// It is not part of the MCP spec — the protocol discovers everything through
// `initialize` — but a human pointing a browser at the endpoint gets something
// better than 405, and story 104 asks for a manifest at this URL.
type manifest struct {
	Name                       string       `json:"name"`
	Version                    string       `json:"version"`
	ProtocolVersion            string       `json:"protocolVersion"`
	SupportedProtocolVersions  []string     `json:"supportedProtocolVersions"`
	Capabilities               capabilities `json:"capabilities"`
	Transport                  string       `json:"transport"`
	Tools                      []Tool       `json:"tools"`
	NotificationStreamComment  string       `json:"notificationStream"`
	AuthenticationInstructions string       `json:"authentication"`
}

// HandleGet serves GET /mcp.
//
// With `Accept: text/event-stream` it opens the server-to-client notification
// stream the transport defines; otherwise it returns the manifest. Overloading
// the verb this way is what lets one URL satisfy both of the story's GET
// requirements without inventing a second path.
func (h *Handler) HandleGet(c fiber.Ctx) error {
	if strings.Contains(c.Get("Accept"), "text/event-stream") {
		return h.handleSSE(c)
	}

	return c.JSON(manifest{
		Name:                      ServerName,
		Version:                   ServerVersion,
		ProtocolVersion:           PreferredProtocolVersion,
		SupportedProtocolVersions: supportedProtocolVersions,
		Capabilities: capabilities{
			Tools:     &toolsCapability{},
			Resources: &resourcesCapability{},
		},
		Transport: "streamable-http",
		Tools:     h.server.toolList,
		NotificationStreamComment: "GET this URL with Accept: text/event-stream to open the " +
			"notification stream. The server sends no notifications yet, so the stream carries " +
			"only keep-alives, and it is closed after the server's write timeout — reconnect.",
		AuthenticationInstructions: "Send an MCP access token as `Authorization: Bearer " +
			mcptoken.TokenPrefix + "…`. Mint one under Settings → MCP Access.",
	})
}

// handleSSE opens the notification stream.
//
// The server has no change feed yet, so the stream carries only keep-alives —
// which is a legitimate implementation of the transport, not a stub: the client
// holds the connection open and would receive notifications the moment there
// are any. It is documented as such in the manifest rather than pretended
// otherwise.
func (h *Handler) handleSSE(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	sessionID := c.Get(headerSessionID)
	if sessionID != "" && h.server.Sessions().Get(sessionID, userID) == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error":   "session_not_found",
			"message": "Unknown or expired MCP session; send initialize again.",
		})
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	// Proxies that buffer would defeat the point of a stream.
	c.Set("X-Accel-Buffering", "no")

	// The stream writer runs after this handler returns, so nothing inside it
	// may touch the fiber context — everything it needs is captured here.
	deadline := time.Now().Add(h.streamLifetime)
	keepAlive := sseKeepAlive
	if h.streamLifetime < keepAlive {
		// Otherwise a short-lived stream would emit nothing between the opening
		// comment and the close, and a client watching for traffic would see a
		// silent connection drop.
		keepAlive = h.streamLifetime / 2
	}
	if keepAlive <= 0 {
		keepAlive = time.Second
	}

	return c.SendStreamWriter(func(w *bufio.Writer) {
		// An initial comment flushes headers immediately, so a client waiting
		// for the stream to "open" is not left hanging until the first
		// keep-alive.
		if _, err := w.WriteString(": connected\n\n"); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}

		ticker := time.NewTicker(keepAlive)
		defer ticker.Stop()
		for range ticker.C {
			if time.Now().After(deadline) {
				return
			}
			// A write or flush error is how a closed connection surfaces here;
			// it is the loop's exit condition, not a fault to report.
			if _, err := w.WriteString(": keepalive\n\n"); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}
		}
	})
}

// HandleDelete serves DELETE /mcp, terminating a session.
//
// Deleting an unknown session is 204 rather than 404: the client asked for the
// session to be gone, and it is.
func (h *Handler) HandleDelete(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	sessionID := c.Get(headerSessionID)
	if sessionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "missing_session",
			"message": "Mcp-Session-Id header is required to terminate a session.",
		})
	}
	h.server.Sessions().Delete(sessionID, userID)
	return c.SendStatus(fiber.StatusNoContent)
}
