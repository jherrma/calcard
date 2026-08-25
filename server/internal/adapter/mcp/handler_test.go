package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/config"
	domainuser "github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/usecase/mcptoken"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// httpEnv wires the transport over the tool server for end-to-end HTTP tests.
type httpEnv struct {
	*testEnv
	app     *fiber.App
	jwt     *authadapter.JWTManager
	handler *Handler
}

func newHTTPEnv(t *testing.T) *httpEnv {
	t.Helper()
	e := newTestEnv(t)

	jwtCfg := &config.JWTConfig{Secret: "test-secret", AccessExpiry: time.Hour, RefreshExpiry: time.Hour}
	jwtManager := authadapter.NewJWTManager(jwtCfg)

	handler := NewHandler(
		e.server,
		mcptoken.NewAuthenticateUseCase(e.mcpTokens, e.userRepo),
		jwtManager,
		e.userRepo,
		e.securityLg,
		30*time.Second,
	)

	app := fiber.New()
	group := app.Group("/mcp", handler.Authenticate())
	group.Post("/", handler.HandleMessage)
	group.Get("/", handler.HandleGet)
	group.Delete("/", handler.HandleDelete)

	return &httpEnv{testEnv: e, app: app, jwt: jwtManager, handler: handler}
}

// mintToken creates a real MCP token for a user and returns the raw secret.
func (h *httpEnv) mintToken(userID uint, name string) string {
	h.t.Helper()
	out, err := mcptoken.NewCreateUseCase(h.mcpTokens, h.securityLg).
		Execute(h.ctx(), userID, mcptoken.CreateInput{Name: name})
	require.NoError(h.t, err)
	return out.Token
}

// post sends a JSON-RPC body with the given bearer credential.
func (h *httpEnv) post(credential, body string, headers map[string]string) *http.Response {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(h.t, err)
	return resp
}

func decodeBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &out), "body was: %s", body)
	return out
}

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test","version":"1"}}}`

func TestTransportRejectsMissingAndMalformedCredentials(t *testing.T) {
	h := newHTTPEnv(t)

	for name, credential := range map[string]string{
		"no credential":      "",
		"garbage bearer":     "not-a-real-token",
		"unknown mcp token":  mcptoken.TokenPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"jwt-shaped garbage": "eyJhbGciOiJIUzI1NiJ9.e30.wrong",
	} {
		resp := h.post(credential, initializeBody, nil)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "%s must be refused", name)
		assert.Contains(t, resp.Header.Get("WWW-Authenticate"), "Bearer",
			"%s: clients rely on WWW-Authenticate to discover the scheme", name)
	}
}

func TestTransportAcceptsMCPToken(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	token := h.mintToken(alice.ID, "laptop")

	resp := h.post(token, initializeBody, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	sessionID := resp.Header.Get(headerSessionID)
	require.NotEmpty(t, sessionID, "initialize must hand back a session id header")

	body := decodeBody(t, resp)
	result := body["result"].(map[string]interface{})
	assert.Equal(t, "2025-06-18", result["protocolVersion"])
	assert.Equal(t, ServerName, result["serverInfo"].(map[string]interface{})["name"])
	assert.NotEmpty(t, result["instructions"])
}

func TestTransportAcceptsJWT(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")

	accessToken, _, err := h.jwt.GenerateAccessToken(alice.UUID, alice.Email)
	require.NoError(t, err)

	resp := h.post(accessToken, initializeBody, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"story 104 specifies JWT auth; it must keep working alongside MCP tokens")
}

func TestRevokedTokenStopsWorkingImmediately(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	token := h.mintToken(alice.ID, "laptop")

	require.Equal(t, http.StatusOK, h.post(token, initializeBody, nil).StatusCode)

	tokens, err := h.mcpTokens.ListByUserID(h.ctx(), alice.ID)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.NoError(t, h.mcpTokens.Revoke(h.ctx(), tokens[0].ID))

	assert.Equal(t, http.StatusUnauthorized, h.post(token, initializeBody, nil).StatusCode,
		"revocation must take effect on the very next request")
}

func TestExpiredTokenIsRefused(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")

	past := time.Now().Add(-time.Hour)
	raw, hash, prefix, err := mcptoken.Generate()
	require.NoError(t, err)
	require.NoError(t, h.mcpTokens.Create(h.ctx(), &domainuser.MCPToken{
		UUID:        "expired-token-uuid",
		UserID:      alice.ID,
		Name:        "old",
		TokenHash:   hash,
		TokenPrefix: prefix,
		ExpiresAt:   &past,
		CreatedAt:   past,
	}))

	assert.Equal(t, http.StatusUnauthorized, h.post(raw, initializeBody, nil).StatusCode)
}

func TestInactiveUsersTokenIsRefused(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	token := h.mintToken(alice.ID, "laptop")

	alice.IsActive = false
	require.NoError(t, h.userRepo.Update(h.ctx(), alice))

	assert.Equal(t, http.StatusUnauthorized, h.post(token, initializeBody, nil).StatusCode)
}

func TestTokenLastUsedIsRecorded(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	token := h.mintToken(alice.ID, "laptop")

	require.Equal(t, http.StatusOK, h.post(token, initializeBody, nil).StatusCode)

	tokens, err := h.mcpTokens.ListByUserID(h.ctx(), alice.ID)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.NotNil(t, tokens[0].LastUsedAt, "the settings page shows when a token was last used")
}

func TestNotificationGets202WithAnEmptyBody(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	token := h.mintToken(alice.ID, "laptop")

	for name, body := range map[string]string{
		"single notification": `{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		"batch of only notifications": `[{"jsonrpc":"2.0","method":"notifications/initialized"},` +
			`{"jsonrpc":"2.0","method":"notifications/whatever"}]`,
	} {
		resp := h.post(token, body, nil)
		require.Equal(t, http.StatusAccepted, resp.StatusCode, "%s", name)

		raw, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		// REVERT PROOF: c.SendStatus fills an empty body with the status text,
		// and 202 is not a status fasthttp forbids a body on — so switching back
		// puts "Accepted" here, and a client doing `if (res.ok) res.json()`
		// throws on a response that was supposed to be empty.
		assert.Empty(t, raw, "%s: a 202 for a notification must carry no body, got %q", name, raw)
	}
}

func TestUnknownSessionIsA404SoClientsReinitialize(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	token := h.mintToken(alice.ID, "laptop")

	resp := h.post(token, `{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		map[string]string{headerSessionID: "definitely-not-a-session"})
	assert.Equal(t, http.StatusNotFound, resp.StatusCode,
		"404 is the spec's signal to re-initialize; anything else makes clients give up")
}

func TestSessionOfAnotherUserIsRejected(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	bob := h.newUser("bob@example.com")

	aliceSession := h.post(h.mintToken(alice.ID, "a"), initializeBody, nil).Header.Get(headerSessionID)
	require.NotEmpty(t, aliceSession)

	resp := h.post(h.mintToken(bob.ID, "b"), `{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		map[string]string{headerSessionID: aliceSession})
	assert.Equal(t, http.StatusNotFound, resp.StatusCode,
		"a leaked session id must not be usable by another account")
}

func TestFullSessionLifecycleOverHTTP(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	cal := h.newCalendar(alice.ID, "Work")
	token := h.mintToken(alice.ID, "laptop")

	// 1. initialize
	initResp := h.post(token, initializeBody, nil)
	require.Equal(t, http.StatusOK, initResp.StatusCode)
	session := initResp.Header.Get(headerSessionID)
	require.NotEmpty(t, session)
	sessionHeader := map[string]string{headerSessionID: session}

	// 2. notifications/initialized — a notification, so no body comes back
	notifyResp := h.post(token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, sessionHeader)
	assert.Equal(t, http.StatusAccepted, notifyResp.StatusCode)

	// 3. tools/list
	listResp := h.post(token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, sessionHeader)
	require.Equal(t, http.StatusOK, listResp.StatusCode)
	tools := decodeBody(t, listResp)["result"].(map[string]interface{})["tools"].([]interface{})
	assert.Len(t, tools, 13)

	// 4. tools/call
	callBody := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_event","arguments":{` +
		`"calendar_id":"` + cal.UUID + `","title":"Team Standup",` +
		`"start":"2026-03-06T09:00:00Z","end":"2026-03-06T09:30:00Z"}}}`
	callResp := h.post(token, callBody, sessionHeader)
	require.Equal(t, http.StatusOK, callResp.StatusCode)
	result := decodeBody(t, callResp)["result"].(map[string]interface{})
	assert.NotEqual(t, true, result["isError"])
	assert.Contains(t, result["content"].([]interface{})[0].(map[string]interface{})["text"], "Team Standup")

	// 5. DELETE terminates the session
	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(headerSessionID, session)
	delResp, err := h.app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, delResp.StatusCode)

	// 6. the session is gone
	assert.Equal(t, http.StatusNotFound,
		h.post(token, `{"jsonrpc":"2.0","id":4,"method":"ping"}`, sessionHeader).StatusCode)
}

func TestPostRejectsMalformedBodies(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	token := h.mintToken(alice.ID, "laptop")

	for name, body := range map[string]string{
		"empty":        "",
		"not json":     "{{{",
		"empty batch":  "[]",
		"broken batch": `[{"jsonrpc":`,
	} {
		resp := h.post(token, body, nil)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "%s must be a 400", name)
	}
}

func TestBatchRequestsAreAnswered(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	token := h.mintToken(alice.ID, "laptop")

	// MCP dropped batching in 2025-06-18, but JSON-RPC allows it and older
	// clients send it, so the transport still answers.
	body := `[{"jsonrpc":"2.0","id":1,"method":"ping"},` +
		`{"jsonrpc":"2.0","method":"notifications/initialized"},` +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`
	resp := h.post(token, body, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var responses []map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &responses))
	require.Len(t, responses, 2, "the notification in the batch must not be answered")
}

func TestGetReturnsManifest(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	token := h.mintToken(alice.ID, "laptop")

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := h.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body := decodeBody(t, resp)
	assert.Equal(t, ServerName, body["name"])
	assert.Equal(t, PreferredProtocolVersion, body["protocolVersion"])
	assert.Equal(t, "streamable-http", body["transport"])
	assert.Len(t, body["tools"], 13)
	assert.Contains(t, body["authentication"], mcptoken.TokenPrefix)
}

func TestGetWithEventStreamAcceptOpensAStream(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	token := h.mintToken(alice.ID, "laptop")

	// A short-lived handler so the stream ends promptly instead of holding the
	// test open for the keep-alive interval.
	h.handler.streamLifetime = 50 * time.Millisecond

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := h.app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), ": connected",
		"the opening comment is what flushes headers so a client sees the stream open")
}

func TestDeleteWithoutSessionHeaderIsABadRequest(t *testing.T) {
	h := newHTTPEnv(t)
	alice := h.newUser("alice@example.com")
	token := h.mintToken(alice.ID, "laptop")

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := h.app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStreamLifetimeTracksTheWriteTimeout(t *testing.T) {
	// fasthttp applies the write timeout to the whole streamed response, so the
	// stream must end just before it — a clean close is end-of-stream to the
	// client, an aborted write is an error it has to recover from.
	assert.Equal(t, 28*time.Second, streamLifetime(30*time.Second))
	assert.Equal(t, defaultStreamLifetime, streamLifetime(0), "no timeout means the default bound applies")
	assert.Equal(t, defaultStreamLifetime, streamLifetime(2*time.Hour), "the default is also a ceiling")
	assert.Equal(t, 500*time.Millisecond, streamLifetime(time.Second), "a timeout too short to carry a stream still yields a valid one")
}
