package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
)

// maxLoggedBody caps how much of one request body is written to the log. DAV
// clients send small XML/vCard/iCalendar documents, so this is generous, yet it
// keeps a large upload from flooding the log.
const maxLoggedBody = 64 * 1024

// sensitiveHeaders are never logged in clear text: they carry credentials.
var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
}

// payloadPath reports whether request bodies on path may be logged. Only the
// DAV surface qualifies: the point is to see what CalDAV/CardDAV clients send.
// Everything else (login, password change, token minting under /api) carries
// secrets in the body, so it gets metadata only.
func payloadPath(path string) bool {
	return path == "/dav" || strings.HasPrefix(path, "/dav/") ||
		strings.HasPrefix(path, "/.well-known/")
}

// PayloadLogger returns a debugging middleware that logs every request: method,
// URL, remote address, headers (credentials redacted) and, on DAV paths, the
// body, plus the response status. It is opt-in (CALDAV_LOG_PAYLOADS) because it
// writes user data (contacts, events) to the log.
func PayloadLogger(logger *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		path := c.Path()

		attrs := []any{
			slog.String("method", c.Method()),
			slog.String("url", c.OriginalURL()),
			slog.String("remote", c.IP()),
			slog.Any("headers", redactHeaders(c.GetReqHeaders())),
			slog.Int("body_bytes", len(c.Body())),
		}
		if payloadPath(path) {
			attrs = append(attrs, slog.String("body", renderBody(c.Body())))
		}

		err := c.Next()

		attrs = append(attrs,
			slog.Int("status", c.Response().StatusCode()),
			slog.Duration("latency", time.Since(start)),
		)
		logger.Info("http payload", attrs...)
		return err
	}
}

func redactHeaders(in map[string][]string) http.Header {
	out := make(http.Header, len(in))
	for k, v := range in {
		if sensitiveHeaders[strings.ToLower(k)] {
			out[k] = []string{"[REDACTED]"}
			continue
		}
		out[k] = v
	}
	return out
}

// renderBody returns the body as a log-safe string, truncated to maxLoggedBody.
// Non-UTF-8 content (a binary attachment, a photo in a vCard is base64 so
// usually fine) is summarised rather than dumped.
func renderBody(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	truncated := false
	if len(b) > maxLoggedBody {
		b, truncated = b[:maxLoggedBody], true
	}
	s := string(b)
	if !utf8.ValidString(s) {
		return "[binary body omitted]"
	}
	if truncated {
		s += "…[truncated]"
	}
	return s
}
