#!/bin/bash
set -e

# Change directory to the server root (where the script's parent's parent is)
cd "$(dirname "$0")/.."

echo "Building CalCard server..."

# Create bin directory if it doesn't exist
mkdir -p bin

# Build the binary
go build -o bin/server ./cmd/server

echo "Success! Binary located at bin/server"
