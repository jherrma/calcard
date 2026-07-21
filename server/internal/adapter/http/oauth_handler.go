package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
	State    string `json:"state"`
	Verifier string `json:"verifier,omitempty"` // PKCE code verifier
	Action   string `json:"action"`             // "login" or "link"
	UserID   uint   `json:"user_id,omitempty"`
}

func (h *OAuthHandler) Initiate(c fiber.Ctx) error {
	provider := c.Params("provider")

	url, state, verifier, err := h.initiateUC.Execute(provider, "")
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Store state (and PKCE verifier) in cookie
	ctxData := oauthContext{
		State:    state,
		Verifier: verifier,
		Action:   "login",
	}
	h.setContextCookie(c, ctxData)

	return c.Redirect().To(url)
}

func (h *OAuthHandler) Link(c fiber.Ctx) error {
	provider := c.Params("provider")
	u := c.Locals("user").(*user.User) // Assuming middleware sets this

	url, state, verifier, err := h.initiateUC.Execute(provider, "") // TODO: Redirect URL might differ?
	if err != nil {
		// 409 if provider already linked handled in callback?
		// AC says "Returns 409 if provider account already linked to another user". That's callback.
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	ctxData := oauthContext{
		State:    state,
		Verifier: verifier,
		Action:   "link",
		UserID:   u.ID,
	}
	h.setContextCookie(c, ctxData)

	// This endpoint is JWT-protected, so it is called via an authenticated XHR
	// (a top-level navigation can't carry the Bearer token). Return the provider
	// URL as JSON and let the SPA navigate to it; the signed oauth_context cookie
	// set above travels with that navigation and is read back in Callback.
	return SuccessResponse(c, fiber.Map{"url": url})
}

func (h *OAuthHandler) Callback(c fiber.Ctx) error {
	provider := c.Params("provider")
	code := c.Query("code")
	state := c.Query("state")

	// Validate state. The action is unknown at this point (cookie missing/bad),
	// so send the browser to the login callback page with the error.
	ctxData, err := h.getContextCookie(c)
	if err != nil || ctxData.State != state {
		return h.redirectOAuthError(c, "", "invalid state parameter")
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

	result, err := h.callbackUC.Execute(c.Context(), provider, code, ctxData.Verifier, userAgent, ip, currentUser)
	if err != nil {
		// The browser is here via a provider redirect, so redirect back to the
		// SPA (returning JSON would dead-end, H16). Map the use case's safe,
		// user-actionable sentinels to messages; anything else stays generic so
		// no internal detail leaks into the URL.
		switch {
		case errors.Is(err, authUseCase.ErrRegistrationDisabled):
			return h.redirectOAuthError(c, ctxData.Action, "registration is disabled")
		case errors.Is(err, authUseCase.ErrProviderAlreadyLinked):
			return h.redirectOAuthError(c, ctxData.Action,
				fmt.Sprintf("this %s account is already linked to another user", provider))
		case errors.Is(err, authUseCase.ErrEmailNotVerified):
			return h.redirectOAuthError(c, ctxData.Action,
				fmt.Sprintf("the %s account's email address is not verified; sign in with your password and link %s from your account settings", provider, provider))
		default:
			return h.redirectOAuthError(c, ctxData.Action, "authentication failed")
		}
	}

	// The browser arrived here via a provider redirect, so we must redirect back
	// to the SPA — returning JSON would dead-end on a raw JSON page (H16). Tokens
	// go in the URL *fragment* (after '#'), which browsers never send to the
	// server and which is kept out of most access logs.
	base := strings.TrimRight(h.cfg.BaseURL, "/")

	if ctxData.Action == "link" {
		return c.Redirect().To(fmt.Sprintf("%s/settings/connections#linked=%s", base, url.QueryEscape(provider)))
	}

	// Login: hand the freshly minted tokens to the SPA callback page.
	frag := url.Values{}
	frag.Set("access_token", result.AccessToken)
	frag.Set("refresh_token", result.RefreshToken)
	frag.Set("expires_at", fmt.Sprintf("%d", result.ExpiresAt.Unix()))
	return c.Redirect().To(fmt.Sprintf("%s/auth/oauth/callback#%s", base, frag.Encode()))
}

// redirectOAuthError sends the browser back to the SPA with the error in the URL
// fragment instead of dead-ending on a raw JSON page (H16). Link errors land on
// the settings page; login (and unknown-action) errors land on the OAuth
// callback page, which reads `error` from the fragment.
func (h *OAuthHandler) redirectOAuthError(c fiber.Ctx, action, msg string) error {
	base := strings.TrimRight(h.cfg.BaseURL, "/")
	page := "/auth/oauth/callback"
	if action == "link" {
		page = "/settings/connections"
	}
	frag := url.Values{}
	frag.Set("error", msg)
	return c.Redirect().To(fmt.Sprintf("%s%s#%s", base, page, frag.Encode()))
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
