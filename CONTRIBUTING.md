# Contributing to paperless-gpt

Thank you for considering contributing to **paperless-gpt**! We welcome contributions of all kinds, including bug reports, feature requests, documentation improvements, and code contributions. By participating in this project, you agree to abide by our [Code of Conduct](#code-of-conduct).

## Table of Contents

- [Contributing to paperless-gpt](#contributing-to-paperless-gpt)
  - [Table of Contents](#table-of-contents)
  - [Code of Conduct](#code-of-conduct)
  - [How Can I Contribute?](#how-can-i-contribute)
    - [Reporting Bugs](#reporting-bugs)
    - [Suggesting Enhancements](#suggesting-enhancements)
    - [Submitting Pull Requests](#submitting-pull-requests)
  - [Development Setup](#development-setup)
    - [Prerequisites](#prerequisites)
    - [Backend Setup](#backend-setup)
    - [Frontend Setup](#frontend-setup)
  - [Coding Guidelines](#coding-guidelines)
  - [Style Guidelines](#style-guidelines)
  - [Testing](#testing)
  - [Documentation](#documentation)
  - [Communication](#communication)
  - [License](#license)

---

## Code of Conduct

This project and everyone participating in it is governed by the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behavior to the project maintainer.

## How Can I Contribute?

### Reporting Bugs

If you find a bug, please open an issue on GitHub. Before doing so, please check if the issue has already been reported.

- **Use a clear and descriptive title** for the issue.
- **Describe the steps to reproduce the bug**.
- **Include any relevant logs, screenshots, or code snippets**.
- **Provide information about your environment** (OS, Docker version, LLM provider, etc.).

### Suggesting Enhancements

We appreciate new ideas and enhancements.

- **Search existing issues** to see if your idea has already been discussed.
- **Open a new issue** with a descriptive title.
- **Provide a detailed description** of the enhancement and its benefits.

### Submitting Pull Requests

We welcome pull requests (PRs). Please follow these guidelines:

1. **Fork the repository** and create your branch from `main`.
2. **Ensure your code follows** the [Coding Guidelines](#coding-guidelines).
3. **Write clear commit messages** following the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) specification.
4. **Test your changes** thoroughly.
5. **Update documentation** if necessary.
6. **Submit a pull request** and provide a clear description of your changes.
7. **Link related issues** in your PR description.

## Development Setup

### Prerequisites

- **Go** (version 1.20 or later)
- **Node.js** (version 18 or later)
- **npm** (comes with Node.js)
- **Docker** and **Docker Compose**

### Backend Setup

1. **Clone the repository**:

   ```bash
   git clone https://github.com/icereed/paperless-gpt.git
   cd paperless-gpt
   ```

2. **Set environment variables**:

   - Create a `.env` file in the project root.
   - Set the required environment variables as per the [README](README.md).

3. **Install Go dependencies**:

   ```bash
   go mod download
   ```

4. **Run the backend server**:

   ```bash
   mkdir dist
   touch dist/index.html
   go build
   ./paperless-gpt
   ```

5. **Run the backend server with frontend built in**:

  ```bash
  cd web-app && npm install && npm run build && cp -r dist ..
  go build
  ./paperless-gpt
  ```

### Frontend Setup

1. **Navigate to the frontend directory**:

   ```bash
   cd web-app
   ```

2. **Install Node.js dependencies**:

   ```bash
   npm install
   ```

3. **Start the frontend development server**:

   ```bash
   npm run dev
   ```

The application should now be accessible at `http://localhost:8080`.

## Coding Guidelines

- **Languages**: Go for the backend, TypeScript with React for the frontend.
- **Formatting**:
  - Use `gofmt` or `goimports` for Go code.
  - Use Prettier and ESLint for frontend code (`npm run lint`).
- **Code Structure**:
  - Keep code modular and reusable.
  - Write clear and concise code with comments where necessary.
- **Dependencies**:
  - Manage Go dependencies with `go mod`.
  - Manage frontend dependencies with `npm`.

## Style Guidelines

- **Commit Messages**:

  - Follow the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) format.
    - Examples:
      - `feat: add support for custom server-side prompts`
      - `fix: resolve API pagination issue for tags`
  - Use the imperative mood in the subject line.

- **Branch Naming**:

  - Use descriptive names:
    - `feat/your-feature-name`
    - `fix/issue-number-description`
    - `docs/update-readme`

- **Pull Requests**:

  - Keep PRs focused; avoid unrelated changes.
  - Provide a detailed description of your changes.
  - Reference any related issues (`Closes #123`).

## Testing

- **Backend Tests**:

  - Write unit tests using Go's `testing` and `github.com/stretchr/testify/assert` packages.
  - Run tests with `go test ./...`.

- **Frontend Tests**:

  - Use testing libraries like Jest and React Testing Library.
  - Run tests with `npm run test`.

- **Continuous Integration**:

  - Ensure all tests pass before submitting a PR.

## Documentation

- **Update Documentation**:

  - Update the [README](README.md) and other relevant docs for any user-facing changes.
  - Include usage examples and configuration instructions.

- **Comment Your Code**:

  - Use clear and descriptive comments for complex logic.
  - Document exported functions and methods in Go.

## Communication

- **GitHub Issues**: Use for bug reports, feature requests, and questions.
- **Discussions**: Engage in discussions for broader topics.
- **Contact Maintainer**: For sensitive matters, contact the maintainer via email.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).

---

Thank you for your interest in contributing to paperless-gpt! We value your input and look forward to your contributions.