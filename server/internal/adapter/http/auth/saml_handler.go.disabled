package auth

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	adapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	httpadapter "github.com/jherrma/caldav-server/internal/adapter/http"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
)

type SAMLHandler struct {
	sp         *adapter.SAMLServiceProvider
	loginUC    *authusecase.SAMLLoginUseCase
	metadataUC *authusecase.SAMLMetadataUseCase
}

func NewSAMLHandler(
	sp *adapter.SAMLServiceProvider,
	loginUC *authusecase.SAMLLoginUseCase,
	metadataUC *authusecase.SAMLMetadataUseCase,
) *SAMLHandler {
	return &SAMLHandler{
		sp:         sp,
		loginUC:    loginUC,
		metadataUC: metadataUC,
	}
}

// Metadata (GET /api/v1/auth/saml/metadata)
func (h *SAMLHandler) Metadata(c fiber.Ctx) error {
	meta, err := h.metadataUC.Execute()
	if err != nil {
		return httpadapter.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	c.Set("Content-Type", "application/samlmetadata+xml")
	return c.Send(meta)
}

// Login (GET /api/v1/auth/saml/login)
func (h *SAMLHandler) Login(c fiber.Ctx) error {
	url, err := h.loginUC.InitiateLogin()
	if err != nil {
		return httpadapter.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to initiate SAML login")
	}
	return c.Redirect().To(url)
}

// ACS (POST /api/v1/auth/saml/acs)
func (h *SAMLHandler) ACS(c fiber.Ctx) error {
	samlResponse := c.FormValue("SAMLResponse")
	if samlResponse == "" {
		return httpadapter.ErrorResponse(c, fiber.StatusBadRequest, "Missing SAMLResponse")
	}

	req, err := http.NewRequest("POST", c.Request().URI().String(), nil)
	if err != nil {
		return httpadapter.ErrorResponse(c, fiber.StatusInternalServerError, "Request creation failed")
	}
	req.ParseForm()
	req.Form.Add("SAMLResponse", samlResponse)

	assertion, err := h.sp.Middleware().ServiceProvider.ParseResponse(req, nil)
	if err != nil {
		// Log detailed error
		fmt.Printf("SAML ParseResponse Error: %v\n", err)
		return httpadapter.ErrorResponse(c, fiber.StatusUnauthorized, "SAML authentication failed")
	}

	// Extract nameID and attributes from assertion
	nameID := assertion.Subject.NameID.Value
	attributes := make(map[string][]string)

	// AttributeStatements is a slice, iterate through it
	for _, attrStatement := range assertion.AttributeStatements {
		for _, attr := range attrStatement.Attributes {
			var values []string
			for _, v := range attr.Values {
				values = append(values, v.Value)
			}
			attributes[attr.Name] = values
			if attr.FriendlyName != "" {
				attributes[attr.FriendlyName] = values
			}
		}
	}

	// Delegate to UseCase
	res, err := h.loginUC.HandleACS(c.Context(), nameID, attributes, c.Get("User-Agent"), c.IP())
	if err != nil {
		return httpadapter.ErrorResponse(c, fiber.StatusUnauthorized, err.Error())
	}

	// Redirect to frontend with tokens
	// Assuming frontend is at root / or /auth/callback
	redirectURL := fmt.Sprintf("/auth/callback?access_token=%s&refresh_token=%s&expires_in=%d",
		res.AccessToken, res.RefreshToken, int64(res.ExpiresAt.Sub(time.Now()).Seconds()))

	return c.Redirect().To(redirectURL)
}
