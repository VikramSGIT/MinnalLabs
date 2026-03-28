FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies (needed for cgo, though we try to keep it minimal)
RUN apk add --no-cache gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o iot-server ./cmd/server

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/iot-server .
COPY --from=builder /app/migrations ./migrations

# Expose HTTP port
EXPOSE 8080

# Run the application
CMD ["./iot-server"]
