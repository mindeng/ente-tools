# 实现计划：Ente 孤立对象清理工具

## 背景

ente server 使用 S3 兼容的多数据中心存储（Backblaze、Wasabi、Scaleway）来存储用户文件和缩略图。文件通过 `object_keys` 表与 S3 对象键映射，但可能存在以下情况导致孤立对象：

1. 文件被更新后，旧对象未清理
2. 用户被删除后，对应的对象未清理
3. 其他异常情况导致对象存在但数据库无引用

本工具用于识别和清理这些孤立对象，节省存储成本。

## 数据库表结构关系

```
users (user_id, email, ...)
    ├── collections (collection_id, owner_id, ...)
    │       └── collection_files (collection_id, file_id, ...)
    ├── files (file_id, owner_id, ...)
    │       └── collection_files (collection_id, file_id, ...)
    │       └── object_keys (file_id, o_type, object_key, ...)
    │
object_keys (file_id, o_type, object_key, size, datacenters, is_deleted)
    └── files (file_id) → users (owner_id)
```

## 实现方案

### 工具名称和结构

**工具名**: `ente-cleanup`
**目录**: `./cmd/ente-cleanup/`

```
cmd/ente-cleanup/
├── main.go           # Root command 和 CLI 入口
├── commands.go       # 子命令实现
└── README.md         # 使用说明

internal/storage/     # 新增包
├── client.go         # S3 客户端封装
├── scanner.go        # 存储对象扫描器
└── analyzer.go       # 数据库分析器
```

### 核心命令

#### 1. `list-orphaned` - 列出孤立对象

```
ente-cleanup list-orphaned [flags]
```

**功能**：列出 S3 存储中存在但数据库没有引用的对象

**实现逻辑**：
1. 列出 S3 指定 prefix 下的所有对象
2. 对于每个对象，查询数据库 `object_keys` 表是否存在引用
3. 如果不存在，标记为孤立对象
4. 尝试从数据库获取相关信息（用户 email、collection、文件名）
   - 如果 object_key 格式为 `{userID}/{uuid}`，可尝试匹配历史数据
   - 如果 object_key 格式为 `{userID}/file-data/{fileID}/...`，尝试从 file_data 表获取

**输出格式**：
```
=== 孤立对象列表 ===
发现 15 个孤立对象

Object Key                    | Size       | Estimated User | Collection
------------------------------|------------|----------------|--------------------------
123/4567890-1234-5678-...     | 2.3 MB     | user@example.com | My Album
123/file-data/456/MlData      | 512 KB     | user@example.com | (unknown)
...

总大小: 15.2 MB
```

#### 2. `list-deleted-user-objects` - 列出已删除用户的对象

```
ente-cleanup list-deleted-user-objects [flags]
```

**功能**：列出所有属于已删除用户的 objects（基于对象路径中的用户 ID 前缀）

**实现逻辑**：
1. 首先从数据库 `users` 表获取所有存在的 `user_id`，存入一个 map 或 set
2. 扫描 S3 中的所有对象，从对象路径中提取用户 ID
   - 对象路径格式：`{userID}/...` 或 `{userID}/file-data/{fileID}/...`
3. 检查提取的 user_id 是否存在于 users 表中
4. 如果不存在，则该对象属于已删除用户
5. 可选：对于 `file-data/` 格式的对象，尝试解析文件 ID 并从 `files` 表获取元数据

**输出格式**：
```
=== 已删除用户对象列表 ===
发现 42 个对象

Object Key                    | Size       | User ID  | File Type
------------------------------|------------|----------|--------------------------
123/file-data/456/MlData      | 1.2 MB     | 123      | MlData
456/7890abcdef-1234-5678-...  | 5.6 MB     | 456      | original
...

总大小: 125.4 MB
```

#### 3. `delete-orphaned` - 批量删除孤立对象

```
ente-cleanup delete-orphaned [flags]
```

**功能**：批量删除所有在数据库中没有引用或者已删除用户的 objects

**实现逻辑**：
1. 扫描 S3 中的所有对象
2. 对每个对象检查：
   - 从路径提取 user_id
   - 检查 user_id 是否存在于 users 表
   - 如果不存在，标记为待删除
3. 显示预览列表，要求用户二次确认
4. 逐个删除 S3 对象（考虑多数据中心）
5. 可选：从数据库 `object_keys` 表清理关联记录（如果有）

**交互示例**：
```
找到 42 个待删除对象 (125.4 MB)
是否查看详情? (y/N): y
[显示对象列表]
确认删除这 42 个对象? (yes/NO): yes
删除进度: [==========] 100%
成功删除: 42 个对象
失败: 0 个对象
节省空间: 125.4 MB
```

### 配置和参数

**全局参数**：
- `--config`: 配置文件路径 (默认: `~/.ente/cleanup-config.yaml`)
- `--dry-run`: 模拟运行，不实际执行删除

**子命令参数**：
- `--prefix`: S3 对象前缀 (默认: 空，扫描全部)
- `--datacenter`: 指定数据中心 (默认: 热数据中心)
- `--limit`: 限制返回数量 (默认: 1000)
- `--output`: 输出格式 (table/json/csv)

### 配置文件示例

`~/.ente/cleanup-config.yaml`:
```yaml
# S3 配置
s3:
  endpoints:
    hot: "https://s3.backblaze.com"
    hot-b2: "https://s3.backblaze.com"
    wasabi: "https://s3.wasabisys.com"
    scaleway: "https://s3.scaleway.com"
  buckets:
    hot: "ente-photos"
    wasabi: "ente-photos-wasabi"
    scaleway: "ente-photos-scw"
  access-key: "${S3_ACCESS_KEY}"
  secret-key: "${S3_SECRET_KEY}"

# 数据库配置
database:
  host: "localhost"
  port: 5432
  database: "ente"
  user: "ente"
  password: "${DB_PASSWORD}"
  sslmode: "disable"
```

### 关键文件

**需要修改的文件**：
- `cmd/ente-cleanup/main.go` (新建)
- `cmd/ente-cleanup/commands.go` (新建)
- `internal/storage/client.go` (新建)
- `internal/storage/scanner.go` (新建)
- `internal/storage/analyzer.go` (新建)

**可复用的现有代码**：
- `/Users/min/dev/ente/server/pkg/controller/object_cleanup.go` - S3 删除逻辑参考
- `/Users/min/dev/ente/server/pkg/repo/object.go` - 数据库查询参考
- `/Users/min/dev/ente/server/pkg/utils/s3config/s3config.go` - S3 配置参考

### 依赖项

```go
github.com/spf13/cobra       // CLI 框架
github.com/spf13/viper       // 配置管理
github.com/aws/aws-sdk-go-v2 // AWS S3 SDK v2
github.com/lib/pq           // PostgreSQL 驱动
```

## 实现步骤

1. 创建项目结构和基础文件
2. 实现 S3 客户端封装 (`internal/storage/client.go`)
3. 实现数据库连接和查询 (`internal/storage/analyzer.go`)
4. 实现 `list-orphaned` 命令
5. 实现 `list-deleted-user-objects` 命令
6. 实现 `delete-orphaned` 命令（含确认机制）
7. 添加配置文件支持和环境变量处理
8. 编写 README 和使用文档

## 验证和测试

### 测试步骤

1. **配置测试**：
   ```bash
   # 验证配置文件加载
   ente-cleanup --config test-config.yaml debug
   ```

2. **孤立对象列表测试**：
   ```bash
   # 列出孤立对象（使用 small prefix 测试）
   ente-cleanup list-orphaned --prefix 123/ --limit 10
   ```

3. **已删除用户对象测试**：
   ```bash
   # 列出已删除用户的对象
   ente-cleanup list-deleted-user-objects --limit 10
   ```

4. **删除功能测试**：
   ```bash
   # 先使用 dry-run 模式
   ente-cleanup delete-orphaned --dry-run --prefix test/

   # 实际删除（测试环境）
   ente-cleanup delete-orphaned --prefix test/
   ```

5. **数据库验证**：
   ```bash
   # 验证删除后数据库状态
   psql -h localhost -d ente -c "SELECT COUNT(*) FROM object_keys WHERE is_deleted = FALSE"
   ```

### 输出验证

- `--output json`: 验证 JSON 格式输出正确
- `--output csv`: 验证 CSV 格式可被其他工具处理
- `table` 格式: 验证表格对齐和可读性

## 注意事项

1. **安全性**：
   - 所有删除操作都需要用户二次确认
   - 支持 `--dry-run` 模式进行预览
   - 默认限制操作数量，防止误删

2. **性能考虑**：
   - 使用 S3 分页查询处理大量对象
   - 数据库查询使用索引优化
   - 支持断点续传和进度显示

3. **多数据中心**：
   - 支持指定单个或多个数据中心
   - 删除时考虑对象的复制状态

4. **日志和审计**：
   - 记录所有删除操作到日志文件
   - 支持导出删除记录为报告