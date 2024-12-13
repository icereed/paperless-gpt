# Stage 1: Build the Go binary
FROM golang:1.22-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Install necessary packages
RUN apk add --no-cache \
    git \
    gcc \
    musl-dev \
    mupdf \
    mupdf-dev

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Pre-compile go-sqlite3 to avoid doing this every time
RUN CGO_ENABLED=1 go build -tags musl -o /dev/null github.com/mattn/go-sqlite3

# Now copy the actual source files
COPY *.go .

# Build the binary using caching for both go modules and build cache
RUN CGO_ENABLED=1 GOMAXPROCS=$(nproc) go build -tags musl -o paperless-gpt .

# Stage 2: Build Vite frontend
FROM node:20-alpine AS frontend

# Set the working directory inside the container
WORKDIR /app

# Install necessary packages
RUN apk add --no-cache git

# Copy package.json and package-lock.json
COPY web-app/package.json web-app/package-lock.json ./

# Install dependencies
RUN npm install

# Copy the frontend code
COPY web-app /app/

# Build the frontend
RUN npm run build

# Stage 3: Create a lightweight image with the Go binary and frontend
FROM alpine:latest

# Install necessary runtime dependencies
RUN apk add --no-cache \
    ca-certificates

# Set the working directory inside the container
WORKDIR /app/

# Copy the Go binary from the builder stage
COPY --from=builder /app/paperless-gpt .

# Copy the frontend build
COPY --from=frontend /app/dist /app/web-app/dist

# Expose the port the app runs on
EXPOSE 8080

# Command to run the binary
CMD ["/app/paperless-gpt"]
