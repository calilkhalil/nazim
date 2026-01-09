# Contributing to nazim

Thank you for your interest in contributing to nazim! This document provides guidelines and instructions for contributing.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/your-username/nazim.git`
3. Create a branch: `git checkout -b feature/your-feature-name`
4. Make your changes
5. Test your changes: `make test`
6. Ensure code is formatted: `make fmt`
7. Commit your changes: `git commit -m "Add: your feature description"`
8. Push to your fork: `git push origin feature/your-feature-name`
9. Open a Pull Request

## Development Setup

```sh
# Build the project
make build

# Run tests
make test

# Format code
make fmt

# Lint code (if golangci-lint is installed)
make lint

# Run vet
make vet
```

## Code Style

- Follow Go conventions and idioms
- Use `gofmt` for formatting
- Write clear, descriptive commit messages
- Add comments for exported functions and types
- Keep functions focused and small
- Follow the patterns established in the codebase (similar to basar project)

## Commit Messages

Use clear, descriptive commit messages:
- `Add: feature description` for new features
- `Fix: bug description` for bug fixes
- `Update: what was updated` for updates
- `Refactor: what was refactored` for refactoring
- `Docs: what documentation was added/changed`

## Pull Request Process

1. Ensure your code follows the style guidelines
2. Add tests if applicable
3. Update documentation if needed
4. Ensure all tests pass
5. Describe your changes clearly in the PR description
6. Reference any related issues

## Testing

When adding new features or fixing bugs:
- Add unit tests for new functionality
- Test on multiple platforms (Windows, Linux, macOS) if possible
- Ensure existing tests still pass

## Platform-Specific Considerations

When working on platform-specific code:
- Test on the target platform
- Consider fallback mechanisms (e.g., systemd → cron on Linux)
- Document platform-specific behavior

## Reporting Issues

When reporting issues, please include:
- Description of the problem
- Steps to reproduce
- Expected behavior
- Actual behavior
- Environment (OS, Go version, etc.)
- Relevant logs/output

## Questions?

Feel free to open an issue for questions or discussions about the project.

