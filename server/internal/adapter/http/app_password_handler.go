package http

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/usecase/apppassword"
)

type AppPasswordHandler struct {
	createUC *apppassword.CreateUseCase
	listUC   *apppassword.ListUseCase
	revokeUC *apppassword.RevokeUseCase
	cfg      *config.Config
}

func NewAppPasswordHandler(
	createUC *apppassword.CreateUseCase,
	listUC *apppassword.ListUseCase,
	revokeUC *apppassword.RevokeUseCase,
	cfg *config.Config,
) *AppPasswordHandler {
	return &AppPasswordHandler{
		createUC: createUC,
		listUC:   listUC,
		revokeUC: revokeUC,
		cfg:      cfg,
	}
}

// Create (POST /api/v1/app-passwords)
func (h *AppPasswordHandler) Create(c fiber.Ctx) error {
	var req dto.CreateAppPasswordRequest
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	userUUID, ok := c.Locals("user_uuid").(string)
	if !ok {
		return UnauthorizedResponse(c, "Unauthorized")
	}

	usecaseReq := apppassword.CreateAppPasswordRequest{
		UserUUID: userUUID,
		Name:     req.Name,
		Scopes:   req.Scopes,
	}

	res, err := h.createUC.Execute(c.Context(), usecaseReq)
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create app password")
	}

	response := dto.CreateAppPasswordResponse{
		ID:        res.ID,
		Name:      res.Name,
		Scopes:    res.Scopes,
		CreatedAt: res.CreatedAt,
		Password:  res.Password,
		Credentials: dto.AppPasswordCredentialsResponse{
			Username:  res.Username,
			Password:  res.Password,
			ServerURL: h.cfg.BaseURL,
		},
	}

	return SuccessResponse(c, response)
}

// List (GET /api/v1/app-passwords)
func (h *AppPasswordHandler) List(c fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uint)
	if !ok {
		return UnauthorizedResponse(c, "Unauthorized")
	}

	aps, err := h.listUC.Execute(c.Context(), userID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list app passwords")
	}

	res := dto.ListAppPasswordsResponse{
		AppPasswords: make([]dto.AppPasswordResponse, len(aps)),
	}

	for i, ap := range aps {
		res.AppPasswords[i] = dto.AppPasswordResponse{
			ID:        ap.UUID,
			Name:      ap.Name,
			Scopes:    ap.GetScopes(),
			CreatedAt: ap.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
		if ap.LastUsedAt != nil {
			s := ap.LastUsedAt.Format("2006-01-02T15:04:05Z")
			res.AppPasswords[i].LastUsedAt = &s
		}
		if ap.LastUsedIP != "" {
			res.AppPasswords[i].LastUsedIP = &ap.LastUsedIP
		}
	}

	return SuccessResponse(c, res)
}

// Revoke (DELETE /api/v1/app-passwords/{id})
func (h *AppPasswordHandler) Revoke(c fiber.Ctx) error {
	appPwdUUID := c.Params("id")
	userUUID, ok := c.Locals("user_uuid").(string)
	if !ok {
		return UnauthorizedResponse(c, "Unauthorized")
	}

	if err := h.revokeUC.Execute(c.Context(), userUUID, appPwdUUID); err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to revoke app password")
	}

	return c.SendStatus(fiber.StatusNoContent)
}
