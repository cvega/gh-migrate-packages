# gh-migrate-packages

A GitHub CLI extension to migrate packages between GitHub organizations and enterprises.

## Installation

```bash
gh extension install owner/gh-migrate-packages
```

## Authentication

You'll need personal access tokens with the following scopes:
- `read:packages` for listing and reading packages
- `write:packages` for creating packages
- `delete:packages` for cleaning up packages if needed

## Usage

### Export packages to CSV
```bash
gh migrate-packages export -o SOURCE_ORG -t TOKEN [-p PACKAGE_TYPE]
```

### Migrate packages between organizations
```bash
gh migrate-packages sync \
  -s SOURCE_ORG \
  -t TARGET_ORG \
  -a SOURCE_TOKEN \
  -b TARGET_TOKEN \
  [-p PACKAGE_TYPE]
```

### Supported Package Types
- container (GitHub Container Registry)
- npm
- maven
- nuget
- rubygems

## Development

### Setup
1. Clone the repository
2. Install dependencies: `go mod download`
3. Build: `go build`

### Running Tests
```bash
go test ./...
```
