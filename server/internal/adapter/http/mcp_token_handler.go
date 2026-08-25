package http

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/usecase/mcptoken"
)

// MCPTokenHandler manages the bearer tokens that authenticate MCP clients
// (story 104). These are ordinary JWT-authenticated REST endpoints — a user
// mints an MCP token from the web UI while logged in normally.
type MCPTokenHandler struct {
	createUC *mcptoken.CreateUseCase
	listUC   *mcptoken.ListUseCase
	revokeUC *mcptoken.RevokeUseCase
}

func NewMCPTokenHandler(
	createUC *mcptoken.CreateUseCase,
	listUC *mcptoken.ListUseCase,
	revokeUC *mcptoken.RevokeUseCase,
) *MCPTokenHandler {
	return &MCPTokenHandler{createUC: createUC, listUC: listUC, revokeUC: revokeUC}
}

// createMCPTokenRequest is the wire shape for minting a token. expires_at is an
// RFC 3339 timestamp; omitting it (or sending an empty string) mints a token
// that is valid until revoked.
type createMCPTokenRequest struct {
	Name      string  `json:"name"`
	ExpiresAt *string `json:"expires_at"`
}

// POST /api/v1/mcp-tokens
//
// The response carries the secret in `token`. It is the ONLY time the server
// can produce it — only its SHA-256 is stored — so the UI must treat this
// response as the single delivery.
func (h *MCPTokenHandler) Create(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	var req createMCPTokenRequest
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	input := mcptoken.CreateInput{
		Name:      req.Name,
		IP:        c.IP(),
		UserAgent: c.Get("User-Agent"),
	}
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			return BadRequestResponse(c, "Invalid expires_at format, expected RFC 3339")
		}
		input.ExpiresAt = &t
	}

	output, err := h.createUC.Execute(c.Context(), u.ID, input)
	if err != nil {
		if errors.Is(err, mcptoken.ErrInvalidInput) {
			return BadRequestResponse(c, err.Error())
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create MCP token")
	}

	return c.Status(fiber.StatusCreated).JSON(output)
}

// GET /api/v1/mcp-tokens
//
// Returns the live tokens with their display prefix, never the secret.
func (h *MCPTokenHandler) List(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	tokens, err := h.listUC.Execute(c.Context(), u.ID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list MCP tokens")
	}

	// Serialize through the model's own json tags — TokenHash is `json:"-"`, so
	// the secret material cannot leak through this path by construction. An
	// empty list must render as [] rather than null so the client can iterate
	// it unconditionally.
	if tokens == nil {
		tokens = []user.MCPToken{}
	}
	return c.JSON(fiber.Map{"tokens": tokens})
}

// DELETE /api/v1/mcp-tokens/:id
//
// Revocation takes effect on the next request: the transport looks the token up
// per call and refuses a revoked row.
func (h *MCPTokenHandler) Revoke(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	err := h.revokeUC.Execute(c.Context(), u.ID, c.Params("id"), c.IP(), c.Get("User-Agent"))
	if err != nil {
		if errors.Is(err, mcptoken.ErrTokenNotFound) {
			return ErrorResponse(c, fiber.StatusNotFound, "MCP token not found")
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to revoke MCP token")
	}

	return c.SendStatus(fiber.StatusNoContent)
}
