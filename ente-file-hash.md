 📦 Ente 文件 Hash 计算分析

 ### 1. 算法选择

 - 算法：使用 libsodium 的 crypto_generichash（Blake2b 变体）
 - 输出：Base64 编码的 32 字节哈希值
 - 实现位置：web/packages/base/crypto/libsodium.ts

 ### 2. 核心流程

 上传时的 hash 计算在 upload-service.ts 的 computeHash 函数中：

 ```typescript
   const computeHash = async (uploadItem: UploadItem, worker: CryptoWorker) => {
       const { stream, chunkCount } = await readUploadItem(uploadItem);
       const hashState = await worker.chunkHashInit();  // 初始化

       const streamReader = stream.getReader();
       for (let i = 0; i < chunkCount; i++) {
           const { value: chunk } = await streamReader.read();
           await worker.chunkHashUpdate(hashState , Uint8Array.from(chunk));  // 逐块更新
       }

       return await worker.chunkHashFinal(hashState) ;  // 完成并返回 Base64 hash
   };
 ```

 ### 3. 技术细节

 ┌────────────┬──────────────────────────────────────────────┐
 │ 项目       │ 详情                                         │
 ├────────────┼──────────────────────────────────────────────┤
 │ Chunk 大小 │ 4 MB (streamEncryptionChunkSize)             │
 ├────────────┼──────────────────────────────────────────────┤
 │ 内存占用   │ 仅缓存 1 个 chunk，适合大文件                │
 ├────────────┼──────────────────────────────────────────────┤
 │ Hash 长度  │ 32 字节 (256 位)                             │
 ├────────────┼──────────────────────────────────────────────┤
 │ 编码       │ Base64 (标准 variant)                        │
 ├────────────┼──────────────────────────────────────────────┤
 │ 哈希用途   │ • 上传去重<br>• 文件身份标识<br>• 验证完整性 │
 └────────────┴──────────────────────────────────────────────┘

 ### 4. Live Photo 特殊处理

 Live Photo 由图像 + 视频组成，其 hash 为：

 ```
   hash = `${imageHash}:${videoHash}`
 ```

 - 分别计算图像和视频的 hash
 - 用冒号拼接后作为整体 hash
 - 旧版本会分开存储 imageHash 和 videoHash（已废弃）

 ### 5. 哈希存储位置

 Hash 存储在 FileMetadata.hash 字段（类型 string），例如：

 ```json
   {
     "fileType": 1,
     "title": "IMG_1234.jpg",
     "creationTime": 1709424000000000,
     "modificationTime": 1709424000000000,
     "hash": "pW5qI9v...（Base64）"
   }
 ```

 ### 6. 为什么选择 crypto_generichash？

 - 安全性：基于 Blake2b，抗碰撞
 - 性能：比 SHA-256 更快，适合大文件流式计算
 - 可配置：libsodium 提供 crypto_generichash_BYTES_MAX (最大 64 字节)
 - 单次/流式：两种用法都支持

 ### 7. 去重机制 (upload-service.ts)

 上传时会先对比 hash：

 ```typescript
   const matches = existingFiles.filter((file) =>
       areFilesSame(file, metadata)
   );
 ```

 areFilesSame 检查：
 1. 文件名相同
 2. 文件类型相同
 3. Hash 相同 → 认为是同一文件

 ────────────────────────────────────────────────────────────────────────────────

 📌 结论：Ente 使用 libsodium 的 generichash（Blake2b），4 MB 流式分块计算，输出 Base64 字符串作为文件唯一标识。
