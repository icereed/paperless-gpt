## Release PR Checklist

### Pre-release Checks
- [ ] Version number follows [semantic versioning](https://semver.org/)
- [ ] All CI checks pass
- [ ] Changelog is up to date
- [ ] Documentation reflects new changes
- [ ] All tests pass locally

### Build Requirements
The release build process requires the following dependencies:
- gcc
- musl-dev
- mupdf
- mupdf-dev

### Release Process
1. Tag will trigger automatic release build via GoReleaser
2. Binary will be built with CGO enabled and musl tag
3. Release artifacts will include:
   - Linux x86_64 binary (statically linked)
   - Source code (zip/tar.gz)
   - Binary checksums
   - Docker images

### Post-release
- [ ] Verify binary works on a clean system
- [ ] Test environment variable configuration
- [ ] Check OCR functionality
- [ ] Confirm OpenAI/Ollama integration
- [ ] Update documentation if needed

### Notes
- Current release only supports Linux/amd64 due to CGO and mupdf dependencies
- Users should install system dependencies as documented in README
- Docker remains the recommended installation method for most users
