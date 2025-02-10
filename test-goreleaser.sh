#!/bin/bash
set -ex

# Clean up any previous builds
rm -rf dist/
docker system prune -f

# Step 1: Build the Docker image with verbose output
echo "Step 1: Building Docker image..."
docker build --no-cache -t goreleaser-test -f Dockerfile.goreleaser .

# Step 2: Verify goreleaser installation in container
echo "Step 2: Verifying goreleaser installation..."
docker run --rm goreleaser-test --version

# Step 3: Test goreleaser build with debug output
echo "Step 3: Testing goreleaser build..."
docker run --rm \
  -e GORELEASER_DEBUG=1 \
  -v "$(pwd):/src" \
  goreleaser-test \
  build --debug --snapshot --clean

# Step 4: Check built artifacts
echo "Step 4: Checking build artifacts..."
ls -la dist/

# Step 5: Run full release process
echo "Step 5: Testing full release process..."
docker run --rm \
  -e GORELEASER_DEBUG=1 \
  -v "$(pwd):/src" \
  goreleaser-test \
  release --debug --snapshot --clean

echo "Done! Check the dist/ directory for test artifacts."
