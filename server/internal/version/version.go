// Package version holds the server's release version.
//
// It lives in its own package so that both the MCP handshake and the settings
// API can report it without importing each other.
package version

// Version is the release version of this build. It is a var, not a const,
// because the linker's -X flag can only set string variables: the Docker build
// injects it via
// -X github.com/jherrma/caldav-server/internal/version.Version=<version>
// (see server/Dockerfile). Local builds report "dev".
var Version = "dev"
