# Contributing to AIB

Thanks for your interest in contributing to AIB (Assets in a Box)!

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/<you>/aib.git`
3. Create a branch: `git checkout -b my-feature`
4. Make your changes
5. Run tests: `make test`
6. Run linter: `make lint`
7. Commit and push
8. Open a Pull Request

## Development

```bash
# Build
make build

# Run tests
make test

# Format code
make fmt

# Lint
make lint
```

## Guidelines

- Run `make test` and `make lint` before submitting a PR
- Add tests for new functionality
- Keep commits focused — one logical change per commit
- Follow existing code style and patterns

## Reporting Issues

Open an issue on GitHub with:
- What you expected to happen
- What actually happened
- Steps to reproduce
- AIB version (`aib --version`)

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
