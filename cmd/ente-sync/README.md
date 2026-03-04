# ente-sync

A command-line tool for syncing metadata with Ente.io and managing your photo library.

## Overview

`ente-sync` provides tools to:

- Sync metadata from your Ente library to a local database
- Find files that exist locally but aren't in your Ente library
- Copy missing files to a backup directory (optional)
- Upload photos and videos to your Ente photos library
- Manage multiple Ente accounts

The tool uses [Ente CLI](https://github.com/ente-io/ente-cli) configuration and credentials, ensuring secure authentication without storing passwords.

## Installation

```bash
# Build from source
go build -o ente-sync ./cmd/ente-sync
```

## Prerequisites

You need [Ente CLI](https://github.com/ente-io/ente-cli) installed and configured with at least one account. The tool reads credentials from Ente CLI's database.

## Configuration

`ente-sync` uses the same configuration as Ente CLI:

- **Config file**: `~/.ente/config.yaml` (for API endpoint)
- **Database**: `~/.ente/ente-cli.db` (for accounts and credentials)
- **Keyring**: System keyring for device key storage

No additional configuration is required.

## Usage

### `accounts`

List configured Ente accounts.

```bash
ente-sync accounts
```

Output:
```
Found 1 account(s):
====================================
Email:   user@example.com
User ID: 1234567890
App:     photos
====================================
```

### `collections`

List collections for an account.

```bash
ente-sync collections --account user@example.com
ente-sync collections --account user@example.com --app photos
```

### `sync`

Sync metadata from Ente to a local database. This is required before using `diff` or `upload` commands.

```bash
ente-sync sync --account user@example.com
ente-sync sync --account user@example.com --output ~/backup/ente.db
```

The sync is incremental - subsequent syncs will only pull changed files and collections.

### `diff`

Find files that exist locally but aren't in your Ente library. The comparison is based on file hash (not filename), so duplicates with different names are correctly identified.

```bash
# Find missing files
ente-sync diff /path/to/photos

# With verbose output
ente-sync diff /path/to/photos -v

# Use a custom metadata database
ente-sync diff /path/to/photos --meta-db ~/backup/ente.db
```

Output:
```
============================================================
MISSING FILES ANALYSIS
============================================================

Total files scanned:  1000
Files in ente:        950
Files NOT in ente:    50
Analysis duration:    2.345s

------------------------------------------------------------
MISSING FILES:
------------------------------------------------------------
2024/01/vacation.jpg
2024/02/sunset.jpg
2024/03/album/photo1.jpg
...
```

#### Copying Missing Files

Use the `--copy-to` option to copy missing files to a target directory while maintaining the original directory structure.

```bash
# Copy missing files to backup directory
ente-sync diff /path/to/photos --copy-to /path/to/backup

# Copy with verbose output (shows progress)
ente-sync diff /path/to/photos --copy-to /path/to/backup -v

# Preview what would be copied (dry run)
ente-sync diff /path/to/photos --copy-to /path/to/backup --dry-run

# Use more parallel workers for faster copying
ente-sync diff /path/to/photos --copy-to /path/to/backup -w 8
```

When using `--copy-to`:

- Source directory structure is preserved in the target directory
- File modification times are preserved
- Existing files in the target are skipped
- **Live Photos are handled automatically** - both the image and video components are copied
- Copying uses parallel workers (default: 4) for better performance
- **Scanning and copying happen simultaneously** - files are copied as soon as they're identified
- Real-time progress is shown with verbose output

Copy output with verbose mode:
```
Analyzing /path/to/photos and copying to /path/to/backup...
Using ente metadata from: ~/.ente/metasync.db
Using 4 parallel workers

Scanned: 100 | Copied: 12 | Skipped: 0 | Failed: 0 | File: 2024/01/vacation.jpg

============================================================
COPY RESULT
============================================================

Total files processed: 48
Copied:               48
Skipped (exists):     0
Failed:               0
Duration:             5.234s
```

### `upload`

Upload files to your Ente photos library.

```bash
# Upload a single file
ente-sync upload /path/to/photo.jpg --account user@example.com

# Upload to a specific album
ente-sync upload /path/to/photo.jpg --account user@example.com --album "My Album"

# Upload all files in a directory
ente-sync upload /path/to/photos --account user@example.com

# With verbose output
ente-sync upload /path/to/photo.jpg --account user@example.com -v
```

Files are uploaded to the "uncategorized" collection by default unless you specify an album with `--album`.

The upload command supports:

- **Live Photos**: Automatically detected and uploaded as a single file (ZIP)
- **Motion Photos**: Google Motion Photos are properly handled
- **Thumbnails**: Auto-generated and uploaded
- **Video metadata**: Duration and dimensions extracted and stored
- **Encryption**: All files and metadata are encrypted before upload

### `debug`

Debug Ente CLI configuration.

```bash
ente-sync debug
```

Output:
```
CLI Config Dir: /Users/user/.ente
Config File: /Users/user/.ente/config.yaml
API Endpoint: https://api.ente.io

Database File: /Users/user/.ente/ente-cli.db

--- Device Key ---
Device Key Length: 32 bytes
Device Key (base64): ABC123...
```

## Workflow Example

A typical workflow for finding and backing up files not in Ente:

```bash
# 1. Sync metadata from Ente
ente-sync sync --account user@example.com

# 2. Find files not in Ente
ente-sync diff /Users/user/Pictures

# 3. Preview copying missing files
ente-sync diff /Users/user/Pictures --copy-to /Users/user/Backup/missing --dry-run

# 4. Actually copy the missing files
ente-sync diff /Users/user/Pictures --copy-to /Users/user/Backup/missing
```

## File Hash Caching

The `diff` command maintains a local hash cache to improve performance:

- First run: All files are hashed
- Subsequent runs: Only changed files are re-hashed
- Cache stored in: `.fhash.db` in the scanned directory

This makes subsequent runs much faster for large photo libraries.

## Troubleshooting

### "Device key not found" Error

Make sure Ente CLI is configured and you have access to your keyring:

```bash
# Check if ente CLI is configured
ente-cli --help

# Try logging in again
ente-cli login
```

### "Account not found" Error

List available accounts to verify the email is correct:

```bash
ente-sync accounts
```

### "metasync database not found" Error

Run the sync command first to create the metadata database:

```bash
ente-sync sync --account user@example.com
```

### Upload Fails

Common issues:

- Check that the file type is supported (images and videos only)
- Verify you have sufficient storage quota on Ente
- Ensure the album exists when using `--album` flag

## Development

### Building

```bash
go build -o ente-sync ./cmd/ente-sync
```

### Running Tests

```bash
go test ./...
```

## License

This tool is not officially affiliated with Ente.io. Use at your own risk.