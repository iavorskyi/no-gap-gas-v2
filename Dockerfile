# Build stage
FROM golang:1.24-alpine AS builder

# Install dependencies for chromedp
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/no-gap-gas-v2

# Runtime stage
FROM chromedp/headless-shell:latest

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/no-gap-gas-v2 .

# Run the application
CMD ["./no-gap-gas-v2"]
