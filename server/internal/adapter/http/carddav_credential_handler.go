package http

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/usecase/apppassword"
)

// CardDAVCredentialHandler handles CardDAV credential HTTP requests
type CardDAVCredentialHandler struct {
	createUC *apppassword.CreateCardDAVCredentialUseCase
	listUC   *apppassword.ListCardDAVCredentialsUseCase
	revokeUC *apppassword.RevokeCardDAVCredentialUseCase
}

// NewCardDAVCredentialHandler creates a new CardDAVCredentialHandler
func NewCardDAVCredentialHandler(
	createUC *apppassword.CreateCardDAVCredentialUseCase,
	listUC *apppassword.ListCardDAVCredentialsUseCase,
	revokeUC *apppassword.RevokeCardDAVCredentialUseCase,
) *CardDAVCredentialHandler {
	return &CardDAVCredentialHandler{
		createUC: createUC,
		listUC:   listUC,
		revokeUC: revokeUC,
	}
}

// CreateCardDAVCredentialRequest is the request for creating a CardDAV credential
type CreateCardDAVCredentialRequest struct {
	Name       string  `json:"name"`
	Username   string  `json:"username"`
	Password   string  `json:"password"`
	Permission string  `json:"permission"`
	ExpiresAt  *string `json:"expires_at"`
}

// Create handles POST /api/v1/carddav-credentials
func (h *CardDAVCredentialHandler) Create(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	var req CreateCardDAVCredentialRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid_request",
			"message": "Invalid request body",
		})
	}

	input := apppassword.CreateCardDAVCredentialInput{
		Name:       req.Name,
		Username:   req.Username,
		Password:   req.Password,
		Permission: req.Permission,
		IP:         c.IP(),
		UserAgent:  c.Get("User-Agent"),
	}

	// Parse expires_at if provided
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   "invalid_request",
				"message": "Invalid expires_at format",
			})
		}
		input.ExpiresAt = &t
	}

	output, err := h.createUC.Execute(c.Context(), u.ID, input)
	if err != nil {
		// Check for username conflict
		if err.Error() == "username '"+req.Username+"' is already in use" {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error":   "conflict",
				"message": err.Error(),
			})
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid_request",
			"message": err.Error(),
		})
	}

	return c.Status(fiber.StatusCreated).JSON(output)
}

// List handles GET /api/v1/carddav-credentials
func (h *CardDAVCredentialHandler) List(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	creds, err := h.listUC.Execute(c.Context(), u.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "internal_error",
			"message": "Failed to list credentials",
		})
	}

	// Map to response (without password hash)
	type CredentialResponse struct {
		ID         string  `json:"id"`
		Name       string  `json:"name"`
		Username   string  `json:"username"`
		Permission string  `json:"permission"`
		ExpiresAt  *string `json:"expires_at"`
		CreatedAt  string  `json:"created_at"`
		LastUsedAt *string `json:"last_used_at"`
		LastUsedIP *string `json:"last_used_ip"`
	}

	response := make([]CredentialResponse, len(creds))
	for i, cred := range creds {
		var expiresAt, lastUsedAt, lastUsedIP *string
		if cred.ExpiresAt != nil {
			s := cred.ExpiresAt.Format("2006-01-02T15:04:05Z")
			expiresAt = &s
		}
		if cred.LastUsedAt != nil {
			s := cred.LastUsedAt.Format("2006-01-02T15:04:05Z")
			lastUsedAt = &s
		}
		if cred.LastUsedIP != "" {
			lastUsedIP = &cred.LastUsedIP
		}

		response[i] = CredentialResponse{
			ID:         cred.UUID,
			Name:       cred.Name,
			Username:   cred.Username,
			Permission: cred.Permission,
			ExpiresAt:  expiresAt,
			CreatedAt:  cred.CreatedAt.Format("2006-01-02T15:04:05Z"),
			LastUsedAt: lastUsedAt,
			LastUsedIP: lastUsedIP,
		}
	}

	return c.JSON(fiber.Map{"credentials": response})
}

// Revoke handles DELETE /api/v1/carddav-credentials/:id
func (h *CardDAVCredentialHandler) Revoke(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)
	credUUID := c.Params("id")

	err := h.revokeUC.Execute(c.Context(), u.ID, credUUID, c.IP(), c.Get("User-Agent"))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error":   "not_found",
			"message": "Credential not found",
		})
	}

	return c.SendStatus(fiber.StatusNoContent)
}
