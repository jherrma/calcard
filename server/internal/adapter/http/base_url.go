package http

import (
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/config"
)

// hostPattern accepts what a Host header legitimately carries — a DNS name or
// IPv4 address, or a bracketed IPv6 literal, each with an optional port — and
// nothing that could reshape the URL it is pasted into (no "/", "@", "\",
// whitespace or a second authority).
var hostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?|\[[0-9A-Fa-f:.]+\])(?::[0-9]{1,5})?$`)

// RequestBaseURL returns the public origin (scheme://host[:port], no trailing
// slash) that URLs handed back in an API response are built on.
//
// A configured base_url (CALDAV_BASE_URL) always wins. Without one the origin
// is derived from the request itself: the scheme is https only when this
// server terminated TLS, and the host is the request's Host header — which is
// exactly what the caller just used to reach us. X-Forwarded-Proto and
// X-Forwarded-Host are honoured only with cfg.TrustForwardedHeaders, because
// without a reverse proxy that overwrites them any client can send them. An
// unusable host yields "" (callers then emit a relative URL) rather than an
// origin the client never sent.
//
// Use it ONLY for URLs returned to the caller of this very request. Anything
// that leaves the request — a link in an e-mail, an OAuth redirect URI, a
// stored value — must use cfg.BaseURL: a forged Host header there would let an
// attacker point someone else's password-reset link at their own server.
func RequestBaseURL(c fiber.Ctx, cfg *config.Config) string {
	if cfg.BaseURL != "" {
		return strings.TrimRight(cfg.BaseURL, "/")
	}

	scheme := "http"
	if c.RequestCtx().IsTLS() {
		scheme = "https"
	}
	host := string(c.Request().Host())

	if cfg.TrustForwardedHeaders {
		switch p := strings.ToLower(firstForwardedValue(c.Get("X-Forwarded-Proto"))); p {
		case "http", "https":
			scheme = p
		}
		if h := firstForwardedValue(c.Get("X-Forwarded-Host")); hostPattern.MatchString(h) {
			host = h
		}
	}

	if !hostPattern.MatchString(host) {
		return ""
	}
	return scheme + "://" + host
}

// firstForwardedValue returns the first entry of a comma-separated forwarded
// header: the one the outermost proxy (the one facing the client) set.
func firstForwardedValue(v string) string {
	first, _, _ := strings.Cut(v, ",")
	return strings.TrimSpace(first)
}
