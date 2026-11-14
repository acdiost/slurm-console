# slurmrestd 版本契约

平台支持 v0.0.40、41、42、43、44。`protocol/versions.go` 是版本和能力的唯一目录。

字段核对依据官方源码：

- [Slurm REST 客户端指南和版本生命周期](https://slurm.schedmd.com/rest_clients.html)。对应插件是否安装、加载，由目标 slurmrestd 决定。
- [v0.0.40 parser](https://github.com/SchedMD/slurm/blob/slurm-24-11-5-1/src/plugins/data_parser/v0.0.40/parsers.c)、[v0.0.41 parser](https://github.com/SchedMD/slurm/blob/slurm-24-11-5-1/src/plugins/data_parser/v0.0.41/parsers.c)、[v0.0.42 parser](https://github.com/SchedMD/slurm/blob/slurm-24-11-5-1/src/plugins/data_parser/v0.0.42/parsers.c)。
- [v0.0.43 parser](https://github.com/SchedMD/slurm/blob/slurm-25-11-0-1/src/plugins/data_parser/v0.0.43/parsers.c)、[v0.0.44 parser](https://github.com/SchedMD/slurm/blob/slurm-25-11-0-1/src/plugins/data_parser/v0.0.44/parsers.c)。
- [24.11 Reservation GET](https://github.com/SchedMD/slurm/blob/slurm-24-11-5-1/src/slurmrestd/plugins/openapi/slurmctld/reservations.c)、[25.11 Reservation 写入及版本约束](https://github.com/SchedMD/slurm/blob/slurm-25-11-0-1/src/slurmrestd/plugins/openapi/slurmctld/reservations.c)。

`adapter/versions_test.go` 的 JSON 为人工构造的最小样例，不是现场数据或完整 OpenAPI 快照。覆盖共有的 JOB_INFO/NODE/PARTITION_INFO、数值 set/infinite/number、旧控制器字符串和新布尔状态。旧版本缺少的新可选字段保持未知。

v0.0.40/41 的 QOS 没有 `limits/max/tres/minutes/total`，因此 `group_minutes` 标为不支持，不得呈现为“未设置”，也不得发送该设置。v0.0.40–42 缺少当前 Reservation 写描述符及写接口，允许查询并在准备和发送阶段阻止写入。

`client/discovery_test.go` 检查认证 OpenAPI 发现、完整核心 GET 路径、只在文档 404 时回退、无重定向及写操作不切换版本；`api/slurm_versions_test.go` 检查五版资源路由与平台权限。完整现场验收还需要实际部署每种 parser、slurmctld/slurmdbd 插件、对应资源和授权账号。
