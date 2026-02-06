package http

import (
	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
)

// SystemHandler handles system-level endpoints
type SystemHandler struct {
	cfg          *config.Config
	userRepo     user.UserRepository
	oauthManager authadapter.OAuthProviderManager
	samlEnabled  bool
}

// NewSystemHandler creates a new SystemHandler
func NewSystemHandler(cfg *config.Config, userRepo user.UserRepository, oauthManager authadapter.OAuthProviderManager, samlEnabled bool) *SystemHandler {
	return &SystemHandler{
		cfg:          cfg,
		userRepo:     userRepo,
		oauthManager: oauthManager,
		samlEnabled:  samlEnabled,
	}
}

// Settings returns system settings for the frontend
func (h *SystemHandler) Settings(c fiber.Ctx) error {
	userCount, _ := h.userRepo.Count(c.Context())

	return SuccessResponse(c, fiber.Map{
		"admin_configured":     userCount > 0,
		"smtp_enabled":         h.cfg.SMTP.Host != "",
		"registration_enabled": true,
	})
}

// AuthMethods returns available authentication methods
func (h *SystemHandler) AuthMethods(c fiber.Ctx) error {
	methods := []fiber.Map{
		{
			"id":   "local",
			"type": "local",
			"name": "Email & Password",
		},
	}

	if h.oauthManager != nil {
		for _, name := range h.oauthManager.ListProviders() {
			methods = append(methods, fiber.Map{
				"id":   name,
				"type": "oidc",
				"name": providerDisplayName(name),
				"url":  h.cfg.BaseURL + "/api/v1/auth/oauth/" + name,
			})
		}
	}

	if h.samlEnabled {
		methods = append(methods, fiber.Map{
			"id":   "saml",
			"type": "saml",
			"name": "SSO (SAML)",
			"url":  h.cfg.BaseURL + "/api/v1/auth/saml/login",
		})
	}

	return SuccessResponse(c, fiber.Map{
		"methods": methods,
	})
}

func providerDisplayName(name string) string {
	switch name {
	case "google":
		return "Google"
	case "microsoft":
		return "Microsoft"
	default:
		return "SSO (" + name + ")"
	}
}
