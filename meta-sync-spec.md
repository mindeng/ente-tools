参考 ../ente/cli 的逻辑，编写一个 meta data sync 工具，要求如下:

1. 兼容 ente cli 的配置，可以利用 ente cli 的配置文件中的 API 端点信息，以及 ente account add 生成的账号及 token 信息
2. 可以列出指定账号下的 collection 列表
3. 可以将指定账号下的所有 collection 及照片、视频文件的 meta 信息同步到本地数据库中，具体 meta 信息字段参考 ente export 导出时生成的 .meta 目录下的 json 文件，再额外加上 exif make & model 信息即可。
