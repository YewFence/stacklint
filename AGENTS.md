After changing Go code, run `mise run check` before finishing, make sure the check passes. If the check fails, try to run `mise run fix` to fix the issues automatically. If the check still fails, you may need to fix the issues manually.

This project is in early development and does not require backward compatibility yet. When a cleaner long-term design requires an incompatible change, make the change deliberately instead of preserving compatibility through extra complexity.

## 实现约束

- 语义以 compose-go 为准：插值、合并、include、dotenv 一律复用 compose-go，不自写解析器或正则模拟。镜像引用只用 `distribution/reference`，registry 访问只用 regclient。
- 渲染双视图：同时保留插值后的规范化模型与未插值模型，规则按需要从对应视图读取，不用正则提取插值变量。
- 无来源 diff：不做"合并后字段来自哪个文件"的逐字段追踪。卷命名扫单文件 yaml.v3；env 归类用逐文件节点顺序 + `[[module]].env_group` 前缀按名匹配。
- 渲染输入平铺：`render.env_files` 与所有 `[[module]].ci_env_file` 并列进同一渲染环境，无优先级分层；不读进程环境、不读项目 `.env`。
- 默认失败：无法确认完整性时报错，不输出看似完整的结果；遇到不认识的输入形状直接失败，不猜测语义。
- 确定性：相同输入产生字节一致的输出，所有集合按固定规则排序。
- 配置文件是仓库根的 `stacklint.toml`（非 `.stacklint/`）；段缺失允许，段内未知键报错。
- registry 网络访问只发生在 `images digests`，其余命令不触网。

## 需求与文档

- `docs/design/requirements.md` 是需求单一事实来源，随实现滚动回填定稿决策，不另建设计文档。
- 第一个消费者是一个私有 stacks 仓库（迁移其 `compose-env`/`compose-checks`/`images-digest-check`/`ops-compose`），但 stacklint 必须独立可用，不包含任何特定仓库的路径或命名假设；仓库策略全部来自 `stacklint.toml`。
