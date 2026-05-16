# Define top-level build arguments
ARG VERSION=docker-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

# Stage 1: Build Vite frontend
FROM docker.io/node:24-alpine AS frontend

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

# Stage 2: Build the Go binary
FROM docker.io/golang:1.25.5-alpine3.21 AS builder

# Set the working directory inside the container
WORKDIR /app

# Install necessary packages
RUN apk add --no-cache \
    gcc \
    musl-dev \
    mupdf \
    mupdf-dev \
    sed

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Pre-compile go-sqlite3 to avoid doing this every time
RUN CGO_ENABLED=1 go build -tags musl -o /dev/null github.com/mattn/go-sqlite3

# Copy the frontend build
COPY --from=frontend /app/dist /app/web-app/dist

# Copy the Go source files
COPY *.go .
COPY ocr ./ocr

# Import ARGs from top level
ARG VERSION
ARG COMMIT
ARG BUILD_DATE

# Update version information
RUN sed -i \
    -e "s/devVersion/${VERSION}/" \
    -e "s/devBuildDate/${BUILD_DATE}/" \
    -e "s/devCommit/${COMMIT}/" \
    version.go

# Build the binary using caching for both go modules and build cache
RUN CGO_ENABLED=1 GOMAXPROCS=$(nproc) go build -tags musl -o paperless-gpt .

# Stage 3: Create a lightweight image with just the binary
FROM docker.io/alpine:3.23.0

ENV GIN_MODE=release

# Install necessary runtime dependencies
RUN apk add --no-cache \
    ca-certificates

# Set the working directory inside the container
WORKDIR /app/

# Copy the Go binary from the builder stage
COPY --from=builder /app/paperless-gpt .

# Copy the prompt templates
COPY default_prompts/ /app/default_prompts/

# Expose the port the app runs on
EXPOSE 8080

# Command to run the binary
CMD ["/app/paperless-gpt"]
