package http

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
)

// The 429 messages of the auth limiters. Tests and clients tell the per-IP and
// the per-account limiter apart by them. FailedAuthLimiter answers with the
// per-account one.
const (
	tooManyAttemptsIPMessage      = "Too many attempts. Please try again later."
	tooManyAttemptsAccountMessage = "Too many login attempts for this account. Please try again later."
)

// RateLimiterConfig defines the configuration for rate limiting
type RateLimiterConfig struct {
	Enabled bool
}

// NewIPRateLimiter creates a rate limiter based on IP address
func NewIPRateLimiter(max int, expiration time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: expiration,
		KeyGenerator: func(c fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c fiber.Ctx) error {
			return ErrorResponse(c, fiber.StatusTooManyRequests, tooManyAttemptsIPMessage)
		},
	})
}

// ExtractEmailMiddleware parses the email from the request body and stores it
// in Locals("login_email") for NewEmailRateLimiter. The key is normalised the
// way the account lookups normalise the address (normalizeAuthIdentifier):
// otherwise "Victim@x", "VICTIM@x" and " victim@x" would each get their own
// bucket while reaching the same account and inbox (#193).
func ExtractEmailMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		var body struct {
			Email string `json:"email"`
		}

		// Use c.Body() to peek at the request body without consuming the stream
		// if Fiber's Bind() implementation were to do so.
		if err := json.Unmarshal(c.Body(), &body); err == nil {
			c.Locals("login_email", normalizeAuthIdentifier(body.Email))
		}
		return c.Next()
	}
}

// NewEmailRateLimiter creates a rate limiter based on email address
func NewEmailRateLimiter(max int, expiration time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: expiration,
		KeyGenerator: func(c fiber.Ctx) string {
			email, ok := c.Locals("login_email").(string)
			if !ok || email == "" {
				return c.IP() // Fallback to IP if email missing
			}
			return email
		},
		LimitReached: func(c fiber.Ctx) error {
			return ErrorResponse(c, fiber.StatusTooManyRequests, tooManyAttemptsAccountMessage)
		},
	})
}

// FailedAuthLimiter limits FAILED credential checks per identifier (the user
// name or e-mail address the attempt named, trimmed and lower-cased) in a fixed
// window. It guards DAV Basic auth (#193), which accepts the account's primary
// password on every request and had only the global limiter. There is no
// per-IP bucket: behind a reverse proxy every client has the proxy's IP, so one
// would let anyone throttle every DAV client of the server.
//
// It is not a Fiber limiter middleware because those decide "failed" by the
// final response status, and on /dav that is the DAV handler's: an
// authenticated client's 404 or 412 would count as a failed login, so a
// syncing client could lock itself out.
//
// Admit runs before the credential is checked and counts the attempt; the
// settle func it returns gives the attempt back when the credential turned out
// valid. Counting before the check is what keeps a burst of parallel guesses
// from all passing an empty counter at once. Once an identifier is spent,
// every attempt for it is a 429 until its window ends, a correct password
// included: answering a correct password differently would tell the guesser
// it was right.
type FailedAuthLimiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	entries   map[string]*failedAuthWindow
	nextSweep time.Time
}

type failedAuthWindow struct {
	failures int
	resetAt  time.Time
}

// NewFailedAuthLimiter allows max failed attempts per identifier within
// window.
func NewFailedAuthLimiter(max int, window time.Duration) *FailedAuthLimiter {
	if window <= 0 {
		window = time.Minute
	}
	return &FailedAuthLimiter{
		max:     max,
		window:  window,
		now:     time.Now,
		entries: map[string]*failedAuthWindow{},
	}
}

// Admit is called before a credential is checked. When the identifier has no
// failed attempts left it answers the request with the auth limiters' 429
// (with Retry-After) and returns a nil settle func and the error of writing
// that response. Otherwise the attempt is counted and the caller must call
// settle exactly once: settle(true) when the credential was valid, which gives
// the attempt back, settle(false) when it was not. An empty identifier is a
// bucket of its own, so blank user names cannot run unlimited checks either.
func (l *FailedAuthLimiter) Admit(c fiber.Ctx, identifier string) (settle func(valid bool), err error) {
	// Cloned: the key outlives the request, and a string Fiber hands out
	// (c.Get, c.Params, …) may share the request buffer that the next request
	// overwrites. ToLower and TrimSpace return their input when nothing changes.
	key := strings.Clone(normalizeAuthIdentifier(identifier))

	l.mu.Lock()
	now := l.now()
	l.sweep(now)
	w := l.entries[key]
	if w != nil && now.Before(w.resetAt) && w.failures >= l.max {
		l.mu.Unlock()
		retryAfter := int((w.resetAt.Sub(now) + time.Second - 1) / time.Second)
		c.Set(fiber.HeaderRetryAfter, strconv.Itoa(retryAfter))
		return nil, ErrorResponse(c, fiber.StatusTooManyRequests, tooManyAttemptsAccountMessage)
	}
	if w == nil || !now.Before(w.resetAt) {
		w = &failedAuthWindow{resetAt: now.Add(l.window)}
		l.entries[key] = w
	}
	w.failures++
	counted := w
	l.mu.Unlock()

	var once sync.Once
	return func(valid bool) {
		once.Do(func() {
			if !valid {
				return
			}
			l.mu.Lock()
			defer l.mu.Unlock()
			// Only give the attempt back to the window it was counted in.
			if w := l.entries[key]; w == counted && w.failures > 0 {
				w.failures--
				if w.failures == 0 {
					delete(l.entries, key)
				}
			}
		})
	}, nil
}

// sweep drops expired windows, at most once per window length, so identifiers
// sprayed by a guesser do not accumulate. Called with l.mu held.
func (l *FailedAuthLimiter) sweep(now time.Time) {
	if now.Before(l.nextSweep) {
		return
	}
	for key, w := range l.entries {
		if !now.Before(w.resetAt) {
			delete(l.entries, key)
		}
	}
	l.nextSweep = now.Add(l.window)
}

// normalizeAuthIdentifier folds a user name or e-mail address the way the
// account lookups do (UserRepository.GetByEmail, LoginUseCase,
// RegisterUseCase): trimmed and lower-cased. Variants that reach the same
// account must share one limiter bucket.
func normalizeAuthIdentifier(identifier string) string {
	return strings.ToLower(strings.TrimSpace(identifier))
}

// NewUserRateLimiter creates a rate limiter keyed on the authenticated user.
//
// It must run AFTER an authentication middleware, which is what puts user_id in
// Locals. Keying on the user rather than the IP is the point: an AI assistant
// calls from one address on behalf of one account, so an IP limit would either
// be too loose (a shared host serving many users) or throttle a single
// legitimate conversation. Requests that somehow arrive unauthenticated fall
// back to the IP so the limiter can never be bypassed by dropping the header.
func NewUserRateLimiter(max int, expiration time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: expiration,
		KeyGenerator: func(c fiber.Ctx) string {
			if userID, ok := c.Locals("user_id").(uint); ok {
				return "user:" + strconv.FormatUint(uint64(userID), 10)
			}
			return "ip:" + c.IP()
		},
		LimitReached: func(c fiber.Ctx) error {
			return ErrorResponse(c, fiber.StatusTooManyRequests, "Too many requests. Please slow down.")
		},
	})
}
