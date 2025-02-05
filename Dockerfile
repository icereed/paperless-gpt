# Define top-level build arguments
ARG VERSION=docker-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

# Stage 1: Build Vite frontend
FROM node:22-alpine AS frontend

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
FROM golang:1.23.6-alpine3.21 AS builder

# Set the working directory inside the container
WORKDIR /app

# Package versions for Renovate
# renovate: datasource=repology depName=alpine_3_21/gcc versioning=loose
ENV GCC_VERSION=14.2.0-r4
# renovate: datasource=repology depName=alpine_3_21/musl-dev versioning=loose
ENV MUSL_DEV_VERSION=1.2.5-r8
# renovate: datasource=repology depName=alpine_3_21/mupdf versioning=loose
ENV MUPDF_VERSION=1.24.10-r0
# renovate: datasource=repology depName=alpine_3_21/mupdf-dev versioning=loose
ENV MUPDF_DEV_VERSION=1.24.10-r0
# renovate: datasource=repology depName=alpine_3_21/sed versioning=loose
ENV SED_VERSION=4.9-r2

# Install necessary packages with pinned versions
RUN apk add --no-cache \
    "gcc=${GCC_VERSION}" \
    "musl-dev=${MUSL_DEV_VERSION}" \
    "mupdf=${MUPDF_VERSION}" \
    "mupdf-dev=${MUPDF_DEV_VERSION}" \
    "sed=${SED_VERSION}"

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Pre-compile go-sqlite3 to avoid doing this every time
RUN CGO_ENABLED=1 go build -tags musl -o /dev/null github.com/mattn/go-sqlite3

# Copy the frontend build
COPY --from=frontend /app/dist /app/dist

# Copy the Go source files
COPY *.go .

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
FROM alpine:latest

ENV GIN_MODE=release

# Install necessary runtime dependencies
RUN apk add --no-cache \
    ca-certificates

# Set the working directory inside the container
WORKDIR /app/

# Copy the Go binary from the builder stage
COPY --from=builder /app/paperless-gpt .

# Expose the port the app runs on
EXPOSE 8080

# Command to run the binary
CMD ["/app/paperless-gpt"]
