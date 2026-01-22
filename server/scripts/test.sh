#!/bin/bash
set -e

# Change directory to the server root
cd "$(dirname "$0")/.."

echo "Running all tests..."

# Run all tests in the project with verbose output
go test -v ./...

echo "All tests passed!"
