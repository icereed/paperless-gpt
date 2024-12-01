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

# Copy the rest of the application code
COPY . .

# Build the Go binary with the musl build tag
RUN GOMAXPROCS=4 go build -tags musl -o paperless-gpt .

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
