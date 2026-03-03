# Ente Metadata Sync Tool - Implementation Plan

## Context

在现有的 `ente-hashcmp` 项目中添加一个 metadata sync 工具子命令，参考 `../ente/cli` 的逻辑，实现以下功能：
1. 兼容 ente cli 的配置，可以利用 ente cli 的配置文件中的 API 端点信息，以及 `ente account add` 生成的账号及 token 信息
2. 可以列出指定账号下的 collection 列表
3. 可以将指定账号下的所有 collection 及照片、视频文件的 meta 信息同步到本地 SQLite 数据库中

## 用户额外要求

1. 兼容目前的项目结构，在 `cmd/ente-hashcmp/commands.go` 中添加新命令
2. 如果服务器返回的 metadata 中不包含 exif make/model 信息，可以先忽略，不需要下载文件
3. SQLite 采用 `modernc.org/sqlite` 库，避免 C 依赖

## 现有项目结构

```
ente-hashcmp/
├── cmd/ente-hashcmp/
│   ├── main.go          # 主入口
│   └── commands.go      # 命令定义（在此添加新命令）
├── internal/
│   ├── database/        # bbolt 数据库
│   ├── comparator/
│   ├── hash/
│   ├── livephoto/
│   ├── scanner/
│   └── types/
├── pkg/db/              # DB 路径管理
├── go.mod               # 已有 cobra, bbolt, golang.org/x/crypto
```

## 参考的 ente cli 关键结构

### 1. 配置管理 (`../ente/cli/main.go`)
- 配置文件: `$HOME/.ente/config.yaml`
- 配置项: `endpoint.api`
- 使用 viper 读取

### 2. 账号存储 (`../ente/cli/pkg/account.go`)
- 数据库: `$HOME/.ente/ente-cli.db` (bbolt)
- bucket: `accounts`
- 账号结构 (`model.Account`):
  ```go
  type Account struct {
      Email     string    `json:"email"`
      UserID    int64     `json:"userID"`
      App       api.App   `json:"app"`
      MasterKey EncString `json:"masterKey"`
      SecretKey EncString `json:"secretKey"`
      PublicKey string    `json:"publicKey"`
      Token     EncString `json:"token"`
      ExportDir string    `json:"exportDir"`
  }
  ```
- AccountKey: `{app}-{userID}`

### 3. 密钥管理 (`../ente/cli/pkg/secrets/`)
- DeviceKey 存储在系统 keyring (或 `ENTE_CLI_SECRETS_PATH`)
- 使用 DeviceKey 解密 Account 的加密字段

### 4. API 客户端 (`../ente/cli/internal/api/client.go`)
- 默认 endpoint: `https://api.ente.io`
- Token header: `X-Auth-Token`
- ClientPkg header: `X-Client-Package: io.ente.photos`

### 5. Collection API (`../ente/cli/internal/api/collection.go`)
- `GetCollections(ctx, sinceTime)` → `[]Collection`
- Collection 结构: ID, Owner, EncryptedKey, EncryptedName, MagicMetadata 等

### 6. File API (`../ente/cli/internal/api/files.go`)
- `GetFiles(ctx, collectionID, sinceTime)` → `[]File`
- File 结构: ID, EncryptedKey, Metadata, MagicMetadata, PubicMagicMetadata 等

### 7. 解密逻辑 (`../ente/cli/pkg/mapper/photo.go`)
- Collection key: 用 master key 解密（自己的）或用 sealed box（shared）
- File key: 用 collection key 解密
- Metadata: ChaCha20-Poly1305 解密

### 8. Export Metadata 结构 (`../ente/cli/pkg/model/export/metadata.go`)
```go
type DiskFileMetadata struct {
    Title            string    `json:"title"`
    Description      *string   `json:"description"`
    Location         *Location `json:"location"`
    CreationTime     time.Time `json:"creationTime"`
    ModificationTime time.Time `json:"modificationTime"`
    Info             *Info     `json:"info"`
}
type Info struct {
    ID        int64    `json:"id"`
    Hash      *string  `json:"hash"`
    OwnerID   int64    `json:"ownerID"`
    FileNames []string `json:"fileNames"`
}
```

## Implementation Plan

### 新增文件结构

```
ente-hashcmp/
├── cmd/ente-hashcmp/
│   └── commands.go              # 添加: meta 子命令及 accounts, collections, sync 子命令
├── internal/
│   └── metasync/
│       ├── config.go            # 读取 ente cli 配置 ($HOME/.ente/config.yaml)
│       ├── account.go           # 从 bbolt 读取账号信息并解密
│       ├── api_client.go        # HTTP API 客户端
│       ├── crypto.go            # 解密相关函数 (ChaCha20-Poly1305, sealed box)
│       ├── models.go            # Collection, File 等结构体
│       ├── sync.go              # 同步逻辑
│       └── sqlite.go            # SQLite 数据库操作
```

### 命令行接口

在现有命令基础上添加:

```bash
# 列出 ente cli 中已配置的账号
ente-hashcmp meta-sync accounts

# 列出指定账号的 collections
ente-hashcmp meta-sync list-collections --account <email> [--app photos]

# 同步 metadata 到 SQLite
ente-hashcmp meta-sync sync --account <email> [--app photos] --output <db_path>

# 同步所有账号
ente-hashsync meta-sync sync --all --output <db_path>
```

### SQLite 表结构

```sql
-- Collections table
CREATE TABLE IF NOT EXISTS collections (
    id INTEGER PRIMARY KEY,
    owner_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    is_deleted BOOLEAN DEFAULT FALSE,
    is_shared BOOLEAN DEFAULT FALSE,
    created_at DATETIME,
    updated_at DATETIME,
    synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Files table
CREATE TABLE IF NOT EXISTS files (
    id INTEGER PRIMARY KEY,
    collection_id INTEGER NOT NULL,
    owner_id INTEGER NOT NULL,
    title TEXT,
    description TEXT,
    creation_time DATETIME,
    modification_time DATETIME,
    latitude REAL,
    longitude REAL,
    file_type TEXT,
    file_size INTEGER,
    hash TEXT,
    exif_make TEXT,       -- 服务器返回则存储，否则 NULL
    exif_model TEXT,      -- 服务器返回则存储，否则 NULL
    is_deleted BOOLEAN DEFAULT FALSE,
    synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (collection_id) REFERENCES collections(id)
);

CREATE INDEX idx_files_collection ON files(collection_id);
CREATE INDEX idx_files_creation_time ON files(creation_time);
```

### 依赖添加

在 `go.mod` 中添加:
- `github.com/spf13/viper` - 配置读取
- `github.com/zalando/go-keyring` - 系统 keyring
- `github.com/go-resty/resty/v2` - HTTP 客户端
- `modernc.org/sqlite` - SQLite (纯 Go)

### 实现步骤

1. **config.go**: 使用 viper 读取 `$HOME/.ente/config.yaml` 获取 API endpoint

2. **account.go**:
   - 打开 `$HOME/.ente/ente-cli.db`
   - 从 `accounts` bucket 读取
   - 用 DeviceKey 解密 token 和 keys

3. **api_client.go**:
   - 复用 ente cli 的 HTTP 客户端模式
   - 实现 `GetCollections`, `GetFiles`

4. **crypto.go**:
   - ChaCha20-Poly1305 解密 (参考 `internal/crypto/`)
   - Sealed box 解密 (shared collections)

5. **models.go**:
   - Collection, File, DiskFileMetadata 等结构

6. **sync.go**:
   - 遍历 collections → 解密名称 → 存 SQLite
   - 对每个 collection 获取 files → 解密 metadata → 存 SQLite
   - 支持增量同步 (使用 sinceTime)

7. **sqlite.go**:
   - 初始化表结构
   - Insert/Update collections 和 files

### 验证步骤

1. 确保已配置 ente cli 并添加了账号
2. 运行 `ente-hashcmp meta-sync accounts` 显示账号列表
3. 运行 `ente-hashcmp meta-sync list-collections --account <email>` 列出相册
4. 运行 `ente-hashcmp meta-sync sync --account <email> --output meta.db` 创建数据库
5. 使用 `sqlite3 meta.db "SELECT COUNT(*) FROM collections"` 验证
6. 使用 `sqlite3 meta.db "SELECT COUNT(*) FROM files"` 验证

## 文件修改清单

### 修改文件
1. `cmd/ente-hashcmp/commands.go` - 添加 meta-sync 相关命令
2. `go.mod` / `go.sum` - 添加新依赖

### 新增文件
1. `internal/metasync/config.go`
2. `internal/metasync/account.go`
3. `internal/metasync/api_client.go`
4. `internal/metasync/crypto.go`
5. `internal/metasync/models.go`
6. `internal/metasync/sync.go`
7. `internal/metasync/sqlite.go`
