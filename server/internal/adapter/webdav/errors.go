package webdav

import (
	"errors"
	"reflect"

	gowebdav "github.com/emersion/go-webdav"
	"github.com/gofiber/fiber/v3"
)

// httpErrorType is go-webdav's HTTPError. It lives in an internal package, so
// it can neither be named here nor used as an errors.As target; take the type
// from a value instead.
var httpErrorType = reflect.TypeOf(gowebdav.NewHTTPError(0, nil))

// davErrorStatus returns the HTTP status carried by a go-webdav HTTPError in
// err's chain (what our backends return for "not found", "forbidden", …).
func davErrorStatus(err error) (int, bool) {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if reflect.TypeOf(e) != httpErrorType {
			continue
		}
		v := reflect.ValueOf(e)
		if v.IsNil() {
			return 0, false
		}
		// Guarded so a go-webdav upgrade that renames the field degrades to
		// a 500 instead of a panic (TestDAVErrorStatus catches it).
		if code := v.Elem().FieldByName("Code"); code.IsValid() && code.CanInt() && code.Int() > 0 {
			return int(code.Int()), true
		}
		return 0, false
	}
	return 0, false
}

// serveDAVError answers an error returned by one of our hand-rolled DAV
// handlers. emersion translates a backend HTTPError into its status itself,
// but our own handlers (sync-collection, collection PROPFIND/PROPPATCH) hand
// errors straight to Fiber, whose default handler makes every one a 500 —
// a missing collection came back as 500 "404 Not Found" (F17). Mirror
// emersion's internal ServeError instead: the error's status, its text as a
// plain-text body. Any other error stays a 500.
func serveDAVError(c fiber.Ctx, err error) error {
	if err == nil {
		return nil
	}
	code, ok := davErrorStatus(err)
	if !ok {
		return err
	}
	c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
	return c.Status(code).SendString(err.Error())
}
