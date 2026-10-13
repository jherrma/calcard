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
// The rules, per identifier:
//
//   - Only SETTLED failures count. A check counts once it has finished and
//     failed; a check still in flight counts nothing, and a valid one never
//     counts. So concurrent correct logins cannot spend the budget, however
//     many there are.
//   - Once max failures are counted in the window, every attempt is a 429
//     until the window ends, a correct password included: answering a correct
//     password differently would tell the guesser it was right. The window
//     opens with the first failure.
//   - Checks in flight are capped separately at maxPending (2×max), so a burst
//     cannot start unlimited bcrypt work. Only an attempt over that cap is
//     refused for being concurrent (429, Retry-After 1).
//
// Counting only settled failures means a burst of parallel guesses can
// overshoot the budget by what is in flight: at most max-1+maxPending
// failures (3×max-1) per window instead of max.
type FailedAuthLimiter struct {
	max        int
	maxPending int
	window     time.Duration
	now        func() time.Time

	mu        sync.Mutex
	entries   map[string]*failedAuthEntry
	nextSweep time.Time
}

// failedAuthEntry is one identifier's state. While pending > 0 the entry is
// never removed from the map, so a settle func always finds the entry it was
// admitted on.
type failedAuthEntry struct {
	failures int       // settled failures in the window ending at resetAt
	resetAt  time.Time // set when the window's first failure is counted
	pending  int       // admitted checks not yet settled
}

// failedAuthPendingFactor sets the cap on checks in flight per identifier to
// this many times the failure budget.
const failedAuthPendingFactor = 2

// NewFailedAuthLimiter allows max failed attempts per identifier within window
// and failedAuthPendingFactor×max checks in flight per identifier at once.
func NewFailedAuthLimiter(max int, window time.Duration) *FailedAuthLimiter {
	if max < 1 {
		max = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &FailedAuthLimiter{
		max:        max,
		maxPending: failedAuthPendingFactor * max,
		window:     window,
		now:        time.Now,
		entries:    map[string]*failedAuthEntry{},
	}
}

// Admit is called before a credential is checked. When the identifier has no
// failed attempts left, or already has maxPending checks in flight, it answers
// the request with the auth limiters' 429 (with Retry-After) and returns a nil
// settle func and the error of writing that response. Otherwise the check is
// admitted as pending and the caller must call settle once it knows the
// outcome: settle(true) when the credential was valid (nothing is counted),
// settle(false) when it was not (one failure is counted). Later calls are
// no-ops. An empty identifier is a bucket of its own, so blank user names
// cannot run unlimited checks either.
func (l *FailedAuthLimiter) Admit(c fiber.Ctx, identifier string) (settle func(valid bool), err error) {
	// Cloned: the key outlives the request, and a string Fiber hands out
	// (c.Get, c.Params, …) may share the request buffer that the next request
	// overwrites. ToLower and TrimSpace return their input when nothing changes.
	key := strings.Clone(normalizeAuthIdentifier(identifier))

	l.mu.Lock()
	now := l.now()
	l.sweep(now)
	e := l.entries[key]
	if e == nil {
		e = &failedAuthEntry{}
		l.entries[key] = e
	}
	if e.failures > 0 && !now.Before(e.resetAt) {
		e.failures = 0 // the window is over
	}
	var retryAfter time.Duration
	switch {
	case e.failures >= l.max:
		retryAfter = e.resetAt.Sub(now)
	case e.pending >= l.maxPending:
		retryAfter = time.Second
	default:
		e.pending++
	}
	l.mu.Unlock()
	if retryAfter > 0 {
		c.Set(fiber.HeaderRetryAfter, strconv.Itoa(int((retryAfter+time.Second-1)/time.Second)))
		return nil, ErrorResponse(c, fiber.StatusTooManyRequests, tooManyAttemptsAccountMessage)
	}

	var once sync.Once
	return func(valid bool) {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			now := l.now()
			e.pending--
			if !valid {
				if e.failures == 0 || !now.Before(e.resetAt) {
					e.failures = 0
					e.resetAt = now.Add(l.window)
				}
				e.failures++
			}
			if e.idle(now) && l.entries[key] == e {
				delete(l.entries, key)
			}
		})
	}, nil
}

// idle reports whether the entry holds nothing worth keeping: no check in
// flight and no failure in a window that is still open.
func (e *failedAuthEntry) idle(now time.Time) bool {
	return e.pending == 0 && (e.failures == 0 || !now.Before(e.resetAt))
}

// sweep drops idle entries, at most once per window length, so identifiers
// sprayed by a guesser do not accumulate. Called with l.mu held.
func (l *FailedAuthLimiter) sweep(now time.Time) {
	if now.Before(l.nextSweep) {
		return
	}
	for key, e := range l.entries {
		if e.idle(now) {
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
