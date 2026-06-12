package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
	authUseCase "github.com/jherrma/caldav-server/internal/usecase/auth"
)

type OAuthHandler struct {
	initiateUC *authUseCase.InitiateOAuthUseCase
	callbackUC *authUseCase.OAuthCallbackUseCase
	unlinkUC   *authUseCase.UnlinkProviderUseCase
	listUC     *authUseCase.ListLinkedProvidersUseCase
	cfg        *config.Config
}

func NewOAuthHandler(
	initiateUC *authUseCase.InitiateOAuthUseCase,
	callbackUC *authUseCase.OAuthCallbackUseCase,
	unlinkUC *authUseCase.UnlinkProviderUseCase,
	listUC *authUseCase.ListLinkedProvidersUseCase,
	cfg *config.Config,
) *OAuthHandler {
	return &OAuthHandler{
		initiateUC: initiateUC,
		callbackUC: callbackUC,
		unlinkUC:   unlinkUC,
		listUC:     listUC,
		cfg:        cfg,
	}
}

type oauthContext struct {
	State  string `json:"state"`
	Action string `json:"action"` // "login" or "link"
	UserID uint   `json:"user_id,omitempty"`
}

func (h *OAuthHandler) Initiate(c fiber.Ctx) error {
	provider := c.Params("provider")

	url, state, err := h.initiateUC.Execute(provider, "")
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Store state in cookie
	ctxData := oauthContext{
		State:  state,
		Action: "login",
	}
	h.setContextCookie(c, ctxData)

	return c.Redirect().To(url)
}

func (h *OAuthHandler) Link(c fiber.Ctx) error {
	provider := c.Params("provider")
	u := c.Locals("user").(*user.User) // Assuming middleware sets this

	url, state, err := h.initiateUC.Execute(provider, "") // TODO: Redirect URL might differ?
	if err != nil {
		// 409 if provider already linked handled in callback?
		// AC says "Returns 409 if provider account already linked to another user". That's callback.
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	ctxData := oauthContext{
		State:  state,
		Action: "link",
		UserID: u.ID,
	}
	h.setContextCookie(c, ctxData)

	return c.Redirect().To(url) // Or return JSON with URL if client wants to redirect? Story implies initiation endpoint redirects.
}

func (h *OAuthHandler) Callback(c fiber.Ctx) error {
	provider := c.Params("provider")
	code := c.Query("code")
	state := c.Query("state")

	// Validate state
	ctxData, err := h.getContextCookie(c)
	if err != nil || ctxData.State != state {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid state parameter"})
	}

	// Clear cookie
	c.ClearCookie("oauth_context")

	var currentUser *user.User
	if ctxData.Action == "link" {
		currentUser = &user.User{ID: ctxData.UserID}
	}

	// Get User Agent and IP
	userAgent := string(c.Request().Header.UserAgent())
	ip := c.IP()

	result, err := h.callbackUC.Execute(c.Context(), provider, code, userAgent, ip, currentUser)
	if err != nil {
		// Handle specific errors for 409, etc.
		// For now generic 500 or 400.
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if ctxData.Action == "link" {
		// Redirect to settings?
		// AC says: "Redirect to settings page" for linking.
		// But definition of done says "Users can link...".
		// Story: "Redirect to settings page".
		// If I return JSON, frontend can handle it.
		// But if initiate was a redirect, the browser is here.
		// Detailed AC: "Redirect to settings page".
		return c.Redirect().To("/settings/auth") // Assuming frontend route
	}

	// Login
	return c.Status(http.StatusCreated).JSON(result)
}

func (h *OAuthHandler) Unlink(c fiber.Ctx) error {
	provider := c.Params("provider")
	u := c.Locals("user").(*user.User)

	if err := h.unlinkUC.Execute(c.Context(), u.ID, provider); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(http.StatusNoContent)
}

func (h *OAuthHandler) List(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	providers, hasPassword, err := h.listUC.Execute(c.Context(), u.ID)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"providers":    providers,
		"has_password": hasPassword,
	})
}

func (h *OAuthHandler) setContextCookie(c fiber.Ctx, data oauthContext) {
	b, _ := json.Marshal(data)
	payload := base64.URLEncoding.EncodeToString(b)
	// HMAC-sign so the Action/UserID the callback trusts can't be forged or
	// tampered with (the cookie travels through the browser).
	value := payload + "." + h.cookieMAC(payload)

	c.Cookie(&fiber.Cookie{
		Name:     "oauth_context",
		Value:    value,
		Expires:  time.Now().Add(10 * time.Minute),
		HTTPOnly: true,
		Secure:   h.secureCookies(),
		SameSite: "Lax",
	})
}

func (h *OAuthHandler) getContextCookie(c fiber.Ctx) (*oauthContext, error) {
	val := c.Cookies("oauth_context")
	if val == "" {
		return nil, http.ErrNoCookie
	}

	payload, mac, ok := strings.Cut(val, ".")
	if !ok || !hmac.Equal([]byte(mac), []byte(h.cookieMAC(payload))) {
		return nil, http.ErrNoCookie
	}

	b, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}

	var data oauthContext
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}

	return &data, nil
}

// cookieMAC computes the base64 HMAC-SHA256 of the cookie payload, keyed on the
// JWT secret.
func (h *OAuthHandler) cookieMAC(payload string) string {
	mac := hmac.New(sha256.New, []byte(h.cfg.JWT.Secret))
	mac.Write([]byte(payload))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

// secureCookies reports whether the Secure cookie flag should be set, i.e. when
// the server is reached over HTTPS (TLS terminated here, or an https base URL).
func (h *OAuthHandler) secureCookies() bool {
	return h.cfg.TLS.Enabled || strings.HasPrefix(strings.ToLower(h.cfg.BaseURL), "https://")
}
