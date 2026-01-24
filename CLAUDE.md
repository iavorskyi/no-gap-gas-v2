# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Simple Go service: `github.com/iavorskyi/my-go-service`
- Go version: 1.21.13
- Flat structure for simplicity

## Development Commands

### Building
```bash
go build
```

### Running
```bash
go run .
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run a specific test
go test -v -run TestName

# Run tests with race detection
go test -race ./...
```

### Linting & Formatting
```bash
# Format code
go fmt ./...

# Vet code
go vet ./...
```

### Dependencies
```bash
# Tidy dependencies
go mod tidy
```

## Architecture

Simple, flat structure. Keep all `.go` files in the root directory unless there's a clear need for subdirectories.
