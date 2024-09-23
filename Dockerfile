# Stage 1: Build the Go binary
FROM golang:1.22 AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the rest of the application code
COPY . .

# Build the Go binary
RUN CGO_ENABLED=0 GOOS=linux go build -o paperless-gpt main.go

# Stage 2: Build Vite frontend
FROM node:20 AS frontend

# Set the working directory inside the container
WORKDIR /app

# Copy package.json and package-lock.json

COPY web-app/package.json web-app/package-lock.json ./

# Install dependencies
RUN npm install

# Copy the frontend code
COPY web-app /app/

# Build the frontend
RUN npm run build

# Stage 3: Create a lightweight image with the Go binary
FROM alpine:latest

# Install necessary CA certificates
RUN apk --no-cache add ca-certificates

# Set the working directory inside the container
WORKDIR /root/

# Copy the Go binary from the builder stage
COPY --from=builder /app/paperless-gpt .

# Copy the frontend build
COPY --from=frontend /app/dist /root/web-app/dist

# Expose the port the app runs on
EXPOSE 8080

# Validate that the binary is executable
RUN chmod +x paperless-gpt

# Command to run the binary
CMD ["./paperless-gpt"]