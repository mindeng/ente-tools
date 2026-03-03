# ente-cleanup

A command-line tool for identifying and cleaning up orphan objects in Ente's S3-compatible storage.

## Overview

`ente-cleanup` helps manage storage costs by identifying objects that exist in storage but have no references in the database, or belong to deleted users. This can happen when:

1. Files are updated but old objects are not cleaned up
2. Users are deleted but their objects remain in storage
3. Other exceptional situations leave orphaned objects

The tool checks the following database tables for object references:

- `object_keys` - Contains references to file and thumbnail objects
- `file_data` - Contains references to derived data (ML embeddings, video previews, image previews)
- `temp_objects` - Temporary objects during uploads

S3 object key formats:

- `{userID}/{uuid}-{filename}` - Original file or thumbnail objects
- `{userID}/file-data/{fileID}/mldata` - ML data/embeddings
- `{userID}/file-data/{fileID}/vid_preview/{obj_id}` - Video preview files
- `{userID}/file-data/{fileID}/vid_preview/{obj_id}_playlist` - Video HLS playlist
- `{userID}/file-data/{fileID}/img_preview/{obj_id}` - Image preview files

  ┌───────────────────────────────────────────────────────────┬────────────────┬───────────────────┐
  │                           格式                            │      类型      │       说明        │
  ├───────────────────────────────────────────────────────────┼────────────────┼───────────────────┤
  │ {userID}/{uuid}-{filename}                                │ file/thumbnail │ 原始文件或缩略图  │
  ├───────────────────────────────────────────────────────────┼────────────────┼───────────────────┤
  │ {userID}/file-data/{fileID}/mldata                        │ mldata         │ ML 数据/嵌入      │
  ├───────────────────────────────────────────────────────────┼────────────────┼───────────────────┤
  │ {userID}/file-data/{fileID}/vid_preview/{obj_id}          │ vid_preview    │ 视频预览文件      │
  ├───────────────────────────────────────────────────────────┼────────────────┼───────────────────┤
  │ {userID}/file-data/{fileID}/vid_preview/{obj_id}_playlist │ vid_preview    │ 视频 HLS playlist │
  ├───────────────────────────────────────────────────────────┼────────────────┼───────────────────┤
  │ {userID}/file-data/{fileID}/img_preview/{obj_id}          │ img_preview    │ 图片预览文件      │
  └───────────────────────────────────────────────────────────┴────────────────┴───────────────────┘

## Installation

```bash
# Build from source
go build -o ente-cleanup ./cmd/ente-cleanup
```

## Configuration

Create a configuration file at `~/.ente/cleanup-config.yaml`:

```yaml
# S3 Configuration
s3:
  hot:
    access_key: "${S3_ACCESS_KEY}"
    secret_key: "${S3_SECRET_KEY}"
  hot-b2:
    access_key: "${B2_ACCESS_KEY}"
    secret_key: "${B2_SECRET_KEY}"
  wasabi:
    access_key: "${WASABI_ACCESS_KEY}"
    secret_key: "${WASABI_SECRET_KEY}"
  scaleway:
    access_key: "${SCW_ACCESS_KEY}"
    secret_key: "${SCW_SECRET_KEY}"

  endpoints:
    hot: "https://s3.backblaze.com"
    hot-b2: "https://s3.backblaze.com"
    wasabi: "https://s3.wasabisys.com"
    scaleway: "https://s3.scaleway.com"

  buckets:
    hot: "ente-photos"
    hot-b2: "ente-photos-b2"
    wasabi: "ente-photos-wasabi"
    scaleway: "ente-photos-scw"

  hot_storage:
    primary: "hot"
    secondary: "wasabi"

# Database Configuration
database:
  host: "localhost"
  port: 5432
  database: "ente"
  user: "ente"
  password: "${DB_PASSWORD}"
  sslmode: "disable"
```

Environment variables can be used for sensitive values using `${VAR_NAME}` syntax.

## Usage

### Global Flags

- `--config`: Path to configuration file (default: `~/.ente/cleanup-config.yaml`)
- `--dry-run`: Simulate operations without making changes
- `--datacenter`: S3 datacenter to use (default: `hot`)
- `--prefix`: S3 object key prefix to scan (default: all)
- `--limit`: Maximum number of results (default: 1000)
- `--output`: Output format - `table`, `json`, or `csv` (default: `table`)

### Commands

#### `list-orphaned`

List objects that exist in S3 storage but have no references in the database.

```bash
# List orphan objects (table format)
ente-cleanup list-orphaned

# List orphan objects in JSON format
ente-cleanup list-orphaned --output json

# List orphan objects with a specific prefix
ente-cleanup list-orphaned --prefix 123/

# List up to 100 orphan objects
ente-cleanup list-orphaned --limit 100

# List orphan objects in a specific datacenter
ente-cleanup list-orphaned --datacenter wasabi
```

#### `list-deleted-user-objects`

List objects belonging to users who have been deleted from the database.

```bash
# List deleted user objects
ente-cleanup list-deleted-user-objects

# List deleted user objects with a specific prefix
ente-cleanup list-deleted-user-objects --prefix 123/
```

#### `delete-orphaned`

Delete orphan objects from S3 storage.

**⚠️ WARNING: This operation is irreversible! Always use `--dry-run` first.**

```bash
# Preview what would be deleted (dry run)
ente-cleanup delete-orphaned --dry-run

# Preview with a specific prefix
ente-cleanup delete-orphaned --dry-run --prefix 123/

# Actually delete orphan objects (requires confirmation)
ente-cleanup delete-orphaned

# Delete orphan objects with a specific prefix
ente-cleanup delete-orphaned --prefix 123/
```

When running without `--dry-run`, you'll be shown a preview and must type `yes` to confirm.

#### `stats`

Show database statistics including user, file, and object counts.

```bash
ente-cleanup stats
```

### Datacenter Names

- `hot`: Primary hot storage (Backblaze)
- `hot-b2`: Secondary hot storage (Backblaze)
- `wasabi`: Wasabi storage
- `scaleway`: Scaleway cold storage

### Object Key Format

Ente objects use the following key formats:

- `{userID}/{uuid}-{filename}`: Original file objects
- `{userID}/file-data/{fileID}/{type}`: File data (thumbnails, ML data, etc.)

Examples:

- `123/4567890-1234-5678-90ab-cdef12345678.jpg`
- `123/file-data/456/MlData`
- `123/file-data/456/Thumbnail`

## Output Formats

### Table (default)

```
Key                                    Size      Datacenter  User ID  User Email          Collection  File Type
----                                    ----      ----------  --------  ----------          -----------  ----------
123/abc-123.jpg                         2.3 MB    hot         123      user@example.com   My Photos   original
123/file-data/456/MlData               512 KB    hot         123      user@example.com   (unknown)   MlData
```

### JSON

```json
{
  "count": 2,
  "total": "2.8 MB",
  "limit_reached": false,
  "objects": [
    {
      "key": "123/abc-123.jpg",
      "size": 2411724,
      "size_human": "2.3 MB",
      "datacenter": "hot",
      "user_id": 123,
      "user_email": "user@example.com",
      "collection": "My Photos",
      "file_type": "original"
    }
  ]
}
```

### CSV

```csv
Key,Size,Datacenter,User ID,User Email,Collection,File Type
123/abc-123.jpg,2.3 MB,hot,123,user@example.com,My Photos,original
```

## Safety Considerations

1. **Always use `--dry-run` first**: Preview what would be deleted before running actual deletion
2. **Start with a specific prefix**: Use `--prefix` to limit the scope when testing
3. **Use a low `--limit`**: Start with a small limit to see results quickly
4. **Review output carefully**: Check the list before confirming deletion
5. **Database backup**: Ensure you have a recent database backup before running deletions

## Troubleshooting

### Connection Errors

If you see connection errors:

1. Verify your S3 credentials are correct
2. Check the endpoint URL matches your S3 provider
3. Ensure network connectivity to the S3 endpoint
4. Verify database connection settings

### No Orphan Objects Found

If no orphan objects are found:

1. This may indicate your storage is clean
2. Try scanning without a `--prefix` to scan all objects
3. Verify database connection and data

### Large Number of Orphan Objects

If you find many orphan objects:

1. Use `--dry-run` to preview first
2. Consider using `--prefix` to process in batches
3. Review patterns in the orphan objects to identify root cause

## Development

### Building

```bash
go build -o ente-cleanup ./cmd/ente-cleanup
```

### Running Tests

```bash
go test ./...
```

