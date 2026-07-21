package addressbook

import "context"

// skipPhotoHydrationKey is the context key marking a request as not needing
// PHOTO blobs. It lives in the domain package (which imports nothing) so both
// the webdav adapter and the repository can reference it without an import
// cycle (repository → webdav would be one).
type skipPhotoHydrationKey struct{}

// WithSkipPhotoHydration marks a request context as not needing PHOTO blobs
// (e.g. an ETag-only PROPFIND poll), so object listings can skip re-injecting
// the stored photo into each vCard body.
func WithSkipPhotoHydration(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipPhotoHydrationKey{}, true)
}

// SkipPhotoHydration reports whether PHOTO hydration should be skipped for this
// request.
func SkipPhotoHydration(ctx context.Context) bool {
	v, _ := ctx.Value(skipPhotoHydrationKey{}).(bool)
	return v
}
