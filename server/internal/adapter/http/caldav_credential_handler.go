package http

import (
	"time"

	"github.com/jherrma/caldav-server/internal/adapter/http/dto"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/usecase/apppassword"
)

// CalDAVCredentialHandler handles CalDAV credential HTTP requests
type CalDAVCredentialHandler struct {
	createUC *apppassword.CreateCalDAVCredentialUseCase
	listUC   *apppassword.ListCalDAVCredentialsUseCase
	revokeUC *apppassword.RevokeCalDAVCredentialUseCase
}

// NewCalDAVCredentialHandler creates a new CalDAVCredentialHandler
func NewCalDAVCredentialHandler(
	createUC *apppassword.CreateCalDAVCredentialUseCase,
	listUC *apppassword.ListCalDAVCredentialsUseCase,
	revokeUC *apppassword.RevokeCalDAVCredentialUseCase,
) *CalDAVCredentialHandler {
	return &CalDAVCredentialHandler{
		createUC: createUC,
		listUC:   listUC,
		revokeUC: revokeUC,
	}
}

// Create godoc
// @Summary      Create CalDAV credential
// @Description  Create a new app-specific password/credential for CalDAV access
// @Tags         Credentials
// @Accept       json
// @Produce      json
// @Param        request  body      dto.CreateCalDAVCredentialRequest  true  "Credential details"
// @Success      201      {object}  apppassword.CreateCalDAVCredentialOutput
// @Failure      400      {object}  ErrorResponseBody
// @Failure      409      {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /caldav-credentials [post]
func (h *CalDAVCredentialHandler) Create(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	var req dto.CreateCalDAVCredentialRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid_request",
			"message": "Invalid request body",
		})
	}

	input := apppassword.CreateCalDAVCredentialInput{
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

// List godoc
// @Summary      List CalDAV credentials
// @Description  List all CalDAV credentials for the current user
// @Tags         Credentials
// @Produce      json
// @Success      200  {object}  dto.CalDAVCredentialListResponse
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /caldav-credentials [get]
func (h *CalDAVCredentialHandler) List(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	creds, err := h.listUC.Execute(c.Context(), u.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "internal_error",
			"message": "Failed to list credentials",
		})
	}

	response := make([]dto.CalDAVCredentialResponse, len(creds))
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

		response[i] = dto.CalDAVCredentialResponse{
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

	return c.JSON(dto.CalDAVCredentialListResponse{Credentials: response})
}

// Revoke godoc
// @Summary      Revoke CalDAV credential
// @Description  Revoke/Delete a CalDAV credential
// @Tags         Credentials
// @Param        id   path      string  true  "Credential UUID"
// @Success      204
// @Failure      404  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /caldav-credentials/{id} [delete]
func (h *CalDAVCredentialHandler) Revoke(c fiber.Ctx) error {
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
