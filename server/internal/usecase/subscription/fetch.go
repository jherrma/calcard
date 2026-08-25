// Package subscription mirrors remote iCalendar feeds into read-only calendars
// (story 100).
//
// This is the only part of the server that makes outbound HTTP requests to a
// URL an ordinary user supplies, which makes it the classic server-side request
// forgery (SSRF) surface: without a guard, "subscribe to this calendar" is a
// primitive for reading any URL the server can reach — an internal admin panel,
// a cloud instance-metadata endpoint — and piping the response back to the
// requester through their own calendar. fetch.go is where that is contained;
// everything else in the package assumes a fetch already passed through it.
package subscription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// userAgent identifies the server to feed publishers. A recognizable agent is
// what lets a publisher rate-limit or contact us instead of silently blocking.
const userAgent = "CalCard/1.0 (calendar subscription; +https://github.com/jherrma/calcard)"

// maxRedirects bounds a redirect chain. Every hop is re-validated, so this is
// about bounding work, not about safety.
const maxRedirects = 5

// Fetch errors the use cases distinguish. Everything else is reported to the
// user as the message it carries.
var (
	// ErrInvalidURL covers everything rejected before a request is made:
	// unsupported scheme, missing host, embedded credentials, blocked address.
	ErrInvalidURL = errors.New("invalid feed URL")
	// ErrNotCalendar means the fetch succeeded but the body is not iCalendar.
	ErrNotCalendar = errors.New("the URL did not return iCalendar data")
	// ErrTooLarge means the feed exceeded the configured size cap.
	ErrTooLarge = errors.New("feed is too large")
)

// FeedError is a failure attributable to the remote feed rather than to this
// server: the host would not resolve, the origin answered 503, the body was
// not iCalendar. Its message is written to be shown to the subscription's
// owner verbatim, and never contains the feed URL — a feed URL routinely
// carries a secret token, and showing it back would put that token in the
// subscription list, in logs, and in any screenshot of either.
//
// The distinction is load-bearing at the HTTP boundary: a FeedError is the
// caller's input being wrong (400), while anything else is ours (500).
type FeedError struct{ Message string }

func (e *FeedError) Error() string { return e.Message }

func feedErrorf(format string, a ...interface{}) *FeedError {
	return &FeedError{Message: fmt.Sprintf(format, a...)}
}

// FetchOptions mirrors the operator-facing knobs in config.SubscriptionConfig.
type FetchOptions struct {
	MaxSize           int64
	Timeout           time.Duration
	AllowInsecureURLs bool
	AllowPrivateHosts bool
}

// FetchResult is one feed fetch. On NotModified the body is empty and the
// stored mirror is already current.
type FetchResult struct {
	NotModified  bool
	Body         []byte
	ETag         string
	LastModified string
}

// Fetcher retrieves iCalendar feeds under the configured restrictions.
type Fetcher struct {
	opts   FetchOptions
	client *http.Client
}

// NewFetcher builds a Fetcher whose HTTP client cannot reach a blocked address.
//
// The address check lives in the dialer's Control hook rather than in a
// pre-flight DNS lookup on purpose: Control receives the ALREADY RESOLVED
// "ip:port" immediately before connect, so a hostname that resolves to a public
// address when validated and to 127.0.0.1 when dialed — DNS rebinding, the
// standard bypass for pre-flight checks — is caught. It also covers every
// redirect hop and every address in a multi-A-record round robin for free,
// because each of those is a separate dial.
func NewFetcher(opts FetchOptions) *Fetcher {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.MaxSize <= 0 {
		opts.MaxSize = 5 * 1024 * 1024
	}

	allowPrivate := opts.AllowPrivateHosts
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			if network != "tcp4" && network != "tcp6" {
				return fmt.Errorf("%w: unsupported network %q", ErrInvalidURL, network)
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrInvalidURL, err)
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("%w: unresolvable address", ErrInvalidURL)
			}
			if !allowPrivate && !isPublicIP(ip) {
				return fmt.Errorf("%w: %s is not a publicly routable address", ErrInvalidURL, ip)
			}
			return nil
		},
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConnsPerHost:   2,
		// Feeds are fetched once an hour at most; keeping sockets open to
		// dozens of unrelated publishers buys nothing.
		DisableKeepAlives: true,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("%w: too many redirects", ErrInvalidURL)
			}
			// An https feed that redirects to http downgrades the transport;
			// a redirect to any other scheme is not something we follow at all.
			return validateURL(req.URL, opts.AllowInsecureURLs)
		},
	}

	return &Fetcher{opts: opts, client: client}
}

// NormalizeURL validates a user-supplied feed URL and returns the canonical
// form to store and request.
//
// webcal:// is the scheme calendar publishers advertise for "subscribe to
// this" links; it is not a protocol, just https with a different name that
// makes the OS hand the link to a calendar app. Rewriting it here means a user
// can paste the link exactly as the publisher gave it to them.
func NormalizeURL(raw string, allowInsecure bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: url is required", ErrInvalidURL)
	}

	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "webcals://"):
		raw = "https://" + raw[len("webcals://"):]
	case strings.HasPrefix(lower, "webcal://"):
		raw = "https://" + raw[len("webcal://"):]
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if err := validateURL(u, allowInsecure); err != nil {
		return "", err
	}
	return u.String(), nil
}

// validateURL enforces the scheme/host rules on both the initial URL and every
// redirect target.
func validateURL(u *url.URL, allowInsecure bool) error {
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !allowInsecure {
			return fmt.Errorf("%w: only https:// feeds are allowed", ErrInvalidURL)
		}
	default:
		return fmt.Errorf("%w: unsupported scheme %q", ErrInvalidURL, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: url has no host", ErrInvalidURL)
	}
	if u.User != nil {
		// Credentials in the URL would be stored in cleartext and shown back in
		// the subscription list. Refusing is honest; silently stripping them
		// would produce a subscription that 401s with no explanation.
		return fmt.Errorf("%w: credentials in the URL are not supported", ErrInvalidURL)
	}
	return nil
}

// isPublicIP reports whether ip is a globally routable unicast address.
//
// It is an allow-nothing-special list rather than a deny list of known-bad
// ranges: anything that is not plainly public is refused, so a range nobody
// thought of fails closed.
func isPublicIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 0: // 0.0.0.0/8 "this network"
			return false
		case ip4[0] == 100 && ip4[1]&0xc0 == 64: // 100.64.0.0/10 CGNAT
			return false
		case ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 0: // 192.0.0.0/24 IETF protocol assignments
			return false
		case ip4[0] == 198 && ip4[1]&0xfe == 18: // 198.18.0.0/15 benchmarking
			return false
		case ip4[0] >= 240: // 240.0.0.0/4 reserved, incl. 255.255.255.255
			return false
		}
		return true
	}
	// IPv6: refuse anything outside global unicast (2000::/3). This also
	// excludes the IPv4-mapped and NAT64 ranges, which are ways to spell an
	// IPv4 destination that the To4() branch above would otherwise not see.
	return len(ip) == net.IPv6len && ip[0]&0xe0 == 0x20
}

// Fetch retrieves the feed, sending the stored validators so an unchanged feed
// costs a 304 rather than a full body.
func (f *Fetcher) Fetch(ctx context.Context, feedURL, etag, lastModified string) (*FetchResult, error) {
	u, err := url.Parse(feedURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if err := validateURL(u, f.opts.AllowInsecureURLs); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/calendar, application/calendar+xml;q=0.5, */*;q=0.1")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, sanitizeTransportError(err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotModified {
		// Keep the validators we sent: a 304 is not required to repeat them,
		// and dropping them would turn every subsequent poll into a full GET.
		return &FetchResult{NotModified: true, ETag: etag, LastModified: lastModified}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, feedErrorf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	// Read one byte past the cap so an oversized feed is detected rather than
	// silently truncated — a truncated .ics parses fine and would delete every
	// event past the cut-off from the mirror.
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.opts.MaxSize+1))
	if err != nil {
		return nil, sanitizeTransportError(err)
	}
	if int64(len(body)) > f.opts.MaxSize {
		return nil, fmt.Errorf("%w: over %d bytes", ErrTooLarge, f.opts.MaxSize)
	}

	// Sniff the body rather than trusting Content-Type: feeds in the wild
	// serve .ics as text/html, as application/octet-stream, or (verified
	// against a real publisher) with no Content-Type header at all, so a
	// content-type check rejects working feeds while catching nothing a
	// content check does not.
	if !looksLikeICalendar(body) {
		return nil, ErrNotCalendar
	}

	return &FetchResult{
		Body:         body,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}, nil
}

// looksLikeICalendar reports whether the body opens a VCALENDAR. It scans a
// prefix rather than the whole body so a huge HTML page is rejected cheaply,
// and tolerates a UTF-8 BOM and leading whitespace.
func looksLikeICalendar(body []byte) bool {
	const window = 1024
	head := body
	if len(head) > window {
		head = head[:window]
	}
	s := strings.TrimSpace(strings.TrimPrefix(string(head), "\ufeff"))
	return strings.HasPrefix(strings.ToUpper(s), "BEGIN:VCALENDAR")
}

// sanitizeTransportError turns a transport failure into something safe to show
// the subscription's owner.
//
// A raw *url.Error repeats the full request URL, which for a feed carrying a
// secret token in its path means the token ends up in the subscription list and
// in any screenshot of it. The wrapped cause is kept when it is one of ours.
func sanitizeTransportError(err error) error {
	if errors.Is(err, ErrInvalidURL) {
		// Raised by the dial Control hook or CheckRedirect; the message is
		// ours and carries no URL.
		return unwrapTo(err, ErrInvalidURL)
	}
	if errors.Is(err, context.Canceled) {
		return &FeedError{Message: "refresh cancelled"}
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return &FeedError{Message: "the feed did not respond in time"}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return &FeedError{Message: "the feed's host name could not be resolved"}
	}
	return &FeedError{Message: "could not reach the feed"}
}

// unwrapTo returns the innermost error in err's chain that still matches target
// WITHOUT being target itself, so the caller sees our own message ("10.0.0.5 is
// not a publicly routable address") rather than either the *url.Error wrapper
// that repeats the request URL or the bare sentinel, which says only "invalid
// feed URL" and leaves the user with no idea what was wrong.
func unwrapTo(err, target error) error {
	last := err
	for e := err; e != nil; e = errors.Unwrap(e) {
		if e == target {
			break
		}
		if errors.Is(e, target) {
			last = e
		}
	}
	return last
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
