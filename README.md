# ente-tools

Tools for managing ente.io photos library and file operations.

## Overview

`ente-tools` is a suite of command-line utilities for working with [ente.io](https://ente.io/) and managing local photo collections. It includes tools for file hash calculation, metadata synchronization, and storage cleanup.

## Installation

```bash
# Install all tools
go install github.com/mindeng/ente-tools/cmd/fhash@latest
go install github.com/mindeng/ente-tools/cmd/ente-sync@latest
go install github.com/mindeng/ente-tools/cmd/ente-cleanup@latest
```

- `fhash` - File hash comparison tool
- `ente-sync` - Ente.io sync tool
- `ente-cleanup` - Storage cleanup tool

## Tools

### fhash - File Hash Calculator

Calculate and compare file hashes using Blake2b algorithm (matching ente.io's implementation).

#### Features

- **Blake2b hashing** with Base64 encoding (32-byte output)
- **Live Photo support** - Automatically detects and handles Apple Live Photos
- **Directory scanning** - Recursive scan with SQLite database caching
- **Duplicate detection** - Find identical files across directories
- **Incremental updates** - Only rehash modified files

#### Usage

```bash
# Scan a directory and compute hashes
fhash scan /path/to/photos

# Compare two directories' hash sets
fhash diff /photos/A /photos/B

# Compute hash for a single file
fhash hash /path/to/photo.jpg

# Get the database file path for a directory
fhash dbpath /path/to/photos
```

#### Database

Hashes are cached in a SQLite database located at `.fhash/db` within the scanned directory. This allows for fast re-scans by only updating modified files.

#### Live Photos

Live Photos are detected based on:

- File naming patterns (e.g., `IMG_0001.HEIC` + `IMG_0001.MOV`)
- File size limits (max 20MB per asset)
- Creation time proximity (within 1 day)

The combined hash for a Live Photo is `imageHash:videoHash`.

### ente-sync - Ente.io Synchronization Tool

Sync metadata with ente.io and find files that exist locally but not in your ente library.

#### Features

- **Account management** - List configured ente accounts
- **Collection browsing** - View your albums and collections
- **Metadata sync** - Download and cache ente library metadata
- **Missing file analysis** - Find local files not uploaded to ente
- **File copying** - Copy missing files to a separate directory

#### Requirements

- [ente CLI](https://github.com/ente-io/cli) must be configured with valid credentials
- Keyring access (system keychain or equivalent)

#### Usage

```bash
# List configured ente accounts
ente-sync accounts

# List collections for an account
ente-sync collections --account email@example.com

# Sync metadata from ente to local database
ente-sync sync --account email@example.com --output ~/.ente/metasync.db

# Find files not in ente library
ente-sync diff /path/to/local/photos

# Copy missing files to a target directory
ente-sync diff /path/to/local/photos --copy-to /path/to/missing

# Dry run to see what would be copied
ente-sync diff /path/to/local/photos --copy-to /path/to/missing --dry-run
```

#### ⚠️ Upload Command - Not Ready

The `upload` command exists but is **NOT ready for production use**. Please do not use it.

```bash
# DO NOT USE - Upload feature is not mature yet
ente-sync upload <file|dir>  # ⚠️ NOT READY
```

### ente-cleanup - Storage Cleanup Tool

Identify and clean up orphan objects in Ente's S3-compatible storage.

#### ⚠️ Not Ready for Production Use

This tool is **NOT ready for production use**. Please do not use it.

```bash
# DO NOT USE - This tool is not mature yet
ente-cleanup <command>  # ⚠️ NOT READY
```

#### Features

- **Orphan object detection** - Find objects in storage with no database references
- **Deleted user cleanup** - Find objects belonging to deleted users
- **Statistics** - View database and storage statistics
- **Batch deletion** - Safely delete orphan objects with confirmation

#### Requirements

- Access to Ente's PostgreSQL database
- S3-compatible storage credentials

#### Configuration

Create a configuration file at `~/.ente/cleanup-config.yaml`:

```yaml
database:
  host: localhost
  port: 5432
  database: ente
  user: postgres
  password: secret
  sslmode: disable

s3:
  hot:
    region: us-east-1
    endpoint: https://s3.amazonaws.com
    accessKey: your-key
    secretKey: your-secret
    bucket: ente-hot
```

#### Usage

```bash
# List orphan objects
ente-cleanup list-orphaned --datacenter hot

# List objects belonging to deleted users
ente-cleanup list-deleted-user-objects --datacenter hot

# Delete orphan objects (requires confirmation)
ente-cleanup delete-orphaned --datacenter hot

# Dry run to see what would be deleted
ente-cleanup delete-orphaned --datacenter hot --dry-run

# Show database statistics
ente-cleanup stats
```

## Development

### Project Structure

```
ente-tools/
├── cmd/
│   ├── fhash/          # File hash calculator
│   ├── ente-sync/      # Ente synchronization tool
│   └── ente-cleanup/   # Storage cleanup tool
├── internal/
│   ├── hash/           # Hash computation (Blake2b)
│   ├── livephoto/      # Live Photo detection and handling
│   ├── scanner/        # Directory scanning
│   ├── database/       # SQLite database operations
│   ├── comparator/     # Hash comparison utilities
│   ├── metasync/       # Ente metadata sync
│   ├── upload/         # Upload functionality (WIP)
│   ├── storage/        # S3 storage operations
│   └── types/          # Shared data structures
└── tests/
    └── integration/    # Integration tests
```

### Building

```bash
# Build all tools
make build

# Build specific tool
go build -o bin/fhash ./cmd/fhash
go build -o bin/ente-sync ./cmd/ente-sync
go build -o bin/ente-cleanup ./cmd/ente-cleanup
```

### Testing

```bash
# Run all tests
go test ./...

# Run integration tests
go test ./tests/integration/...
```

## Hash Algorithm

`ente-tools` uses Blake2b hashing with the following parameters to match ente.io's implementation:

| Parameter | Value |
|-----------|-------|
| Algorithm | Blake2b (via golang.org/x/crypto/blake2b) |
| Output size | 32 bytes (256 bits) |
| Encoding | Base64 (standard) |
| Chunk size | 4 MB (streamed) |

For Live Photos, the hash is computed as:

```
hash = imageHash + ":" + videoHash
```

## License

This project is licensed under the GNU Affero General Public License Version 3 (AGPL-3.0). See [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

