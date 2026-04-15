//go:build integration

package integration_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// apiResponse matches the {status, data, ...} envelope returned by most REST
// endpoints (the ones that go through SuccessResponse()). Handlers outside
// that envelope (calendars, events, address books, contacts, import handlers)
// return raw JSON; the helpers below accept both shapes via the `unwrap` flag.
type apiResponse struct {
	Status  string          `json:"status"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// restCall performs an HTTP request to the test server under /api/v1 and
// returns the status code and raw response body. No JSON decoding is done
// here — callers pick the right helper (doJSON / doJSONRaw) below.
func restCall(t *testing.T, method, path string, token string, body any) (int, []byte) {
	t.Helper()
	return rawCall(t, method, baseURL+"/api/v1"+path, token, body, nil)
}

// rawCall is like restCall but takes a full URL and optional extra headers.
// Used by the DAV helpers and for endpoints that live outside /api/v1.
func rawCall(t *testing.T, method, fullURL, token string, body any, headers map[string]string) (int, []byte) {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		switch b := body.(type) {
		case []byte:
			reqBody = bytes.NewReader(b)
		case string:
			reqBody = strings.NewReader(b)
		default:
			buf, err := json.Marshal(b)
			require.NoError(t, err, "marshal body")
			reqBody = bytes.NewReader(buf)
		}
	}

	req, err := http.NewRequest(method, fullURL, reqBody)
	require.NoError(t, err, "build request")
	if body != nil {
		if _, alreadySet := headers["Content-Type"]; !alreadySet {
			// Default to JSON. Callers that need something else pass it in headers.
			if _, isBytes := body.([]byte); !isBytes {
				req.Header.Set("Content-Type", "application/json")
			}
		}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	require.NoError(t, err, "do %s %s", method, fullURL)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read body")
	return resp.StatusCode, respBody
}

// doJSON performs an API call that returns a {status, data} envelope, asserts
// the envelope is successful, and decodes the inner .data into `out` (which
// may be nil to skip decoding).
func doJSON(t *testing.T, method, path, token string, body, out any) int {
	t.Helper()
	status, raw := restCall(t, method, path, token, body)
	if status >= 400 {
		return status
	}
	if len(raw) == 0 {
		return status
	}
	var env apiResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope for %s %s: %v\nbody: %s", method, path, err, string(raw))
	}
	require.Equal(t, "ok", env.Status, "unexpected status in envelope for %s %s: %s", method, path, string(raw))
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			t.Fatalf("decode data for %s %s: %v\nbody: %s", method, path, err, string(env.Data))
		}
	}
	return status
}

// doJSONRaw performs an API call that returns raw JSON (no envelope) and
// decodes the full body into `out`. Used for calendar/event/addressbook/contact
// endpoints. Returns the HTTP status.
func doJSONRaw(t *testing.T, method, path, token string, body, out any) int {
	t.Helper()
	status, raw := restCall(t, method, path, token, body)
	if status >= 400 {
		return status
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode raw for %s %s: %v\nbody: %s", method, path, err, string(raw))
		}
	}
	return status
}

// errorMessage pulls a human-readable message out of whatever shape the error
// response happens to have. Used for assertion diagnostics.
func errorMessage(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return string(raw)
	}
	for _, k := range []string{"message", "error"} {
		if v, ok := obj[k].(string); ok && v != "" {
			return v
		}
	}
	return string(raw)
}

// davCall performs a WebDAV request against /dav using HTTP Basic auth and
// returns the status code, response headers, and body. The emersion/go-webdav
// stack expects real request lifecycles (custom methods like PROPFIND, etc.),
// so we go through the real listener over TCP.
func davCall(t *testing.T, method, path, user, pass string, body string, extraHeaders map[string]string) (int, http.Header, []byte) {
	t.Helper()
	fullURL := baseURL + path
	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, fullURL, reqBody)
	require.NoError(t, err, "dav build request")
	auth := user + ":" + pass
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
	if body != "" {
		if extraHeaders["Content-Type"] == "" {
			req.Header.Set("Content-Type", "application/xml; charset=utf-8")
		}
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	require.NoError(t, err, "dav do %s %s", method, fullURL)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "dav read body")
	return resp.StatusCode, resp.Header, respBody
}

// davURL builds a full /dav URL (including the test host) for the given path
// components, URL-encoding each segment safely.
func davURL(parts ...string) string {
	segments := make([]string, 0, len(parts)+1)
	segments = append(segments, "dav")
	for _, p := range parts {
		segments = append(segments, url.PathEscape(p))
	}
	return "/" + strings.Join(segments, "/")
}
