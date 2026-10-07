# stacklint 需求文档

状态：草案。来源是一个私有 stacks 仓库的设计讨论，以及其现有 Python 工具 `ops-compose`、`compose-checks`、`compose-env`、`images-digest-check` 的实际行为。本文是 stacklint 的需求事实来源；该仓库迁移完成后，只保留仓库策略配置和指向本文的链接。

## 背景

一个私有 stacks 仓库用 Docker Compose 描述全部部署 stack，现有 Python 工具通过 `docker compose config` 子进程渲染并检查配置。这条路线有三类结构性成本：

- 进程开销：插值语义分析需要逐表达式调用 Compose，单个 stack 约 120 次调用、5 秒以上，全仓不可接受。
- 绕路解析：官方 CLI 不暴露 include 图、插值语法结构和来源信息，导致仓库里出现逐行正则模拟 YAML、正则提取插值变量名、手写镜像引用和 dotenv 解析、解析 regctl 报错文本等实现。
- 仓库耦合：发现目录、共享模块约定、端口分组等策略写死在代码里。

stacklint 用 Go 直接复用 compose-go、`distribution/reference` 和 regclient，在进程内完成同样的工作，并把全部仓库策略外置为配置文件。

## 目标

- 一个 CLI 覆盖 Compose 静态检查、环境变量覆盖模板生成与漂移检查、镜像 digest 远端验证。
- 语义以 Compose 官方实现为准：插值、合并、include、dotenv 都复用 compose-go，不自写解析器。
- 仓库策略全部来自配置目录，CLI 本身不包含任何特定仓库的路径或命名。
- 默认失败：无法确认完整性时报错，不输出看似完整的结果；所有例外都显式记录、绑定确切目标、填写原因，并在失效时报告过期。

## 非目标

- 不启动容器、不连接 Docker daemon、不拉取镜像层、不创建 Docker 资源。
- 不读取部署环境的真实输入：不读进程环境参与插值，不读 `.env`、`.env.secrets` 等部署文件。
- 不替代运维主机上的运行时工具，不调用业务系统的 API。
- 不生成 Compose 文件，不修改 Compose 行为。

## 术语

- 仓库：包含 stack 的代码仓库，由配置目录确定根目录。
- stack：一个部署单元，即部署根目录下包含主 Compose 文件的目录。
- 部署根目录：配置中声明的、在其下发现 stack 的目录，例如 `stacks/edge`。
- 共享模块：不可独立部署、被 stack 通过 `include` 引用的 Compose 模块。
- 静态渲染输入：检查时为插值提供值的受版本控制文件，例如 `.env.example`、`.env.ci` 和模块 CI 占位 env。
- 批准：对某条规则的显式例外记录。
- 覆盖模板：stack 目录下自动生成的 `.env.example`，列出可覆盖的部署输入。

## 配置

### 配置目录定位

CLI 从一个配置目录读取全部策略，按以下优先级确定，命中即停止，不回退：

1. 全局参数 `--config-dir <dir>`。
2. 环境变量 `STACKLINT_CONFIG_DIR`。
3. 从当前工作目录向上查找包含 `stacklint.toml` 的目录（通常是仓库根目录）。

显式指定（1 或 2）的目录必须包含 `stacklint.toml`，否则直接失败。找不到配置目录时失败，不使用内置默认策略。

### 配置文件

配置文件位于仓库根目录，无隐藏目录：

- `stacklint.toml`：仓库策略，必需。
- `approvals.toml`：全部批准与豁免记录，可选；不存在等价于没有任何批准。

`stacklint.toml` 中的 `repository.root` 相对配置目录解析，缺省为配置目录本身；其余所有路径都相对仓库根目录解析。未知键、错误类型、重复项均失败。

示意（以一个 stacks 仓库为例，字段名在实现时定稿）：

```toml
[repository]
deployment_roots = ["stacks/edge", "stacks/yewyard", "stacks/stump"]
module_roots = ["stacks/compose"]
compose_filenames = ["compose.yaml", "compose.yml"]

[render]
# 所有渲染输入平铺加载，无优先级分层；不存在的文件跳过
env_files = [".env.example", ".env.ci"]

[[module]]
compose = "stacks/compose/litestream/compose.yaml"
ci_env_file = ".env.ci.litestream"
wiring_file = ".env.litestream"
wiring_template = ".env.litestream.example"
# 可选：env 模板的模块分组前缀，缺省回退到 required_environment.prefix
env_group = "LITESTREAM_"
optional_wiring_variables = ["LITESTREAM_REPLICA_SCOPE"]
# 该模块的服务渲染后这些前缀的变量必须非空
required_environment = { service = "litestream", prefix = "LITESTREAM_" }

[ports]
# 共享根目录与其余每个部署根目录各组成一个分组
shared_roots = ["stacks/edge"]

[bind_address]
default_variable = "BIND_ADDRESS"

[env]
output = ".env.example"

[images]
floating_tags = ["latest", "main"]
private_repositories = ["ghcr.io/yewfence/infisical"]
concurrency = 8
per_registry_concurrency = 4
timeout = "30s"
retries = 2
```

`approvals.toml` 保持既有 `[[image]]`、`[[published_port]]`、`[[bind_address]]`、`[[env_file]]` 的结构和语义，新增 `[[env_exemption]]`，见下文 env 部分。

## 核心能力

### 发现

- 在每个部署根目录下递归查找 `compose_filenames` 中的文件，每个命中即一个 stack。
- 命令的目标参数可以是 stack 目录、Compose 文件或路径片段；目标未匹配任何 stack 时失败。
- 输出顺序按仓库相对路径排序，保证确定性。

### 加载与渲染

- 用 compose-go loader 加载 stack，渲染输入按 `render.env_files` 顺序叠加，再追加所有已注册模块的 CI 占位 env 作为最高优先级。模块 CI 占位 env 无条件加载，不依赖 include 图。
- include 自带的 `env_file`（例如模块 wiring 文件）保持 Compose 原生优先级，低于项目级输入。
- 不读取进程环境，不加载项目目录下的 `.env`。
- 启用全部 profile。
- 同时提供两种视图：插值后的规范化模型，以及未插值的模型（部分规则需要读取原始插值表达式）。未插值视图通过 `LoadModelWithContext` 返回合并后的 YAML 字典，不解码为强类型以保留原始表达式。
- 提供源文件关系：stack 实际引用的全部源文件（include 递归展开），以及每个服务、卷、字段来自哪个源文件。卷命名与 env 来源分组通过扫描单文件 yaml.v3 节点与模块 `env_group` 前缀实现，不做逐字段 diff 追踪。
- 渲染失败的 stack 报告错误并继续检查其他 stack。

### 按 Git ref 加载

- 将指定 ref 下部署根目录与模块根目录中的 Compose 文件和静态渲染输入物化到临时目录后加载，用于增量镜像检查和对外输出历史镜像清单。
- 只读取版本库对象，不触碰工作区。支持 git；jj 仓库通过其 git 后端读取。

### 插值分析

- 复用 compose-go `template` 包求值，通过记录型 `Mapping` 观察每个表达式在不同输入下实际查询的变量和结果，不自写插值语法解析。
- 必填错误使用 `MissingRequiredError` 的结构化字段，不解析错误文本。

### 源文件注释

- 用 yaml.v3 Node 读取源文件注释和位置，用于 `@env` 标记。不解析普通注释的归属。

### 镜像引用

- 用 `distribution/reference` 解析和规范化镜像引用。全项目只有这一处镜像引用解析。

## 命令

所有命令共享全局参数 `--config-dir`。

### `stacklint check [targets...]`

渲染选中的 stack 并执行规则，未给目标时检查全部 stack。规则（与既有 Python 工具 compose-checks 语义一致）：

- 基础结构：渲染结果必须有非空顶层 `name` 和非空 `services`。
- 镜像引用：digest 必须是 `sha256:` 加 64 位小写十六进制；空 tag 和格式错误的 digest 不可批准。未固定 digest 的引用需要 `[[image]]` 批准，绑定 Compose 文件、服务、仓库名和规则，规则为 `major`、`major-minor`、`floating-tag`、`exact-tag` 之一，可带 `tag` 或 `suffix`。已固定 digest 时对应批准视为冗余并报错。
- 服务级 `env_file`：解析后指向 stack 目录 `.env` 的服务级 env_file 需要 `[[env_file]]` 批准；指向其他文件不受此规则约束。
- 模块 wiring：stack include 了已注册模块时，stack 目录必须有对应 wiring 文件，变量集合等于模块模板去掉可选变量；静态渲染输入不得重复声明 wiring 变量；`.env.ci` 不得声明模块 CI 占位变量；未 include 模块却存在 wiring 文件视为孤儿文件。
- 模块必需环境：按 `required_environment` 检查模块服务渲染后的对应变量非空，且未插值视图中至少声明了一个这样的变量。
- 宿主机绑定变量：从未插值视图读取端口的宿主机 IP 插值变量（短语法前缀或长语法 `host_ip`），只能是 `bind_address.default_variable`；其他变量需要 `[[bind_address]]` 批准，变量集合必须完全一致。变量提取使用插值分析能力，不用正则。
- 发布端口唯一：每个分组内同一端口和协议只能发布一次，端口范围展开计算。重复需要 `[[published_port]]` 批准，binding 集合（Compose 文件、服务、宿主机 IP）必须完全一致。
- 卷命名：顶层卷渲染后必须是对象且有非空 `name`，并且 `name` 必须在源文件中显式写出（基于源文件关系判断，不依赖 Compose 自动生成的名字）。
- 过期批准：批准目标不存在、规则恢复默认合规或批准内容与现状不符时报错。局部检查只在批准引用的全部 Compose 文件都在本次范围内时判断过期。

### `stacklint env generate <targets...>` / `stacklint env check [targets...]`

从 Compose 生成每个 stack 的覆盖模板 `.env.example`，运行默认值只在 Compose 中维护。

输入发现：

- 所有字段中的插值引用，包括端口、标签、卷、healthcheck 等，不限于 `environment`。
- `environment` 中无值的直接透传（映射形式 `KEY:` 和列表形式 `- KEY`）。
- `build.args` 中无值的透传，以及顶层 `configs`、`secrets` 的 `environment` 来源。
- 结束时用 compose-go 的整文件变量提取结果核对，发现集合必须覆盖它，否则失败。遇到不认识的输入形状直接失败。

源文件标记：

- `# @env VAR: 说明` 为变量添加说明，可选，可重复，相同说明去重，不同说明按源顺序保留。
- `# @env-source VAR: 提供方` 声明外部提供的输入，例如 `mise run ready`。这类输入只进入末尾的来源参考，不提供赋值入口。同名来源必须一致。
- `COMPOSE_PROJECT_NAME` 自动归属于 Docker Compose，不能另行声明。
- 标记格式错误、引用不存在的变量时失败。

输出语义：

- 固定默认值输出为注释赋值，例如 `# VAR=value`，复制模板不会把默认值固化成覆盖项。
- 无条件必填输出为空赋值，并区分 `:?`（不得为空）与 `?`（允许为空）。
- 无值透传输出为注释空入口，不把“未设置”变成空字符串。
- 嵌套派生、条件分支、替代值、同字段重复引用等复杂情况保留完整原表达式和位置，不计算成固定值，也不把内层必填当成全局必填。例如 `${URL:-https://${DOMAIN:?...}}` 中的 `DOMAIN` 只在 `URL` 未设置时必填。
- 同名变量在不同位置有不同的默认行为时生成失败。现有仓库没有这种写法，若出现，应当先统一 Compose。
- 按源文件和首次出现顺序排列，同一字段内的变量按名称排序，来源参考放在末尾。
- 赋值编码由 stacklint 实现，写出后必须用 compose-go 的 dotenv 解析器读回并与原值完全一致，否则失败。

覆盖范围与豁免：

- `env check` 不给目标时检查全部 stack。每个 stack 必须满足以下之一：已提交的 `.env.example` 与生成结果完全一致；或在 `approvals.toml` 中有一条 `[[env_exemption]]`。
- 豁免绑定 stack 目录，`kind` 为以下之一：
  - `env-file-passthrough`：服务级 `env_file` 透传项目 `.env`，示例需要承载上游应用的大规模接口。前提是 Compose 中确实存在这种透传；不再存在时过期。需要填写原因。
  - `upstream`：`.env.example` 同步自上游项目。需要填写原因和上游来源。
  - `pending`：尚未迁移，或使用了暂不支持的形状（include、overlay 等）。不需要原因。
- 豁免 stack 的 `.env.example` 带有生成头时报错，提示删除豁免或恢复手写文件。
- `env generate` 必须显式给出目标，拒绝对豁免 stack 生成。生成在完整扫描成功后原子写入；拒绝符号链接；失败不覆盖已有文件。
- `env check` 只读，不写任何文件。

多文件（后续阶段）：

- include 的模块输入独立成组；受版本控制的 include `env_file` 固定接线值进入来源参考，不提供赋值入口。
- overlay 的文件组合与输出名由命令或配置显式指定，不凭文件名猜测；补充模板以基础加 overlay 的有效合并结果为准。

### `stacklint images digests [--base <ref> | --full | --ci <provider>]`

远端验证镜像引用，语义与既有 images-digest-check 一致：

- 按完整引用去重并发查询顶层 manifest 或 image index digest，不指定平台。
- 固定 `repo:tag@digest` 时先查 tag；一致即通过；不一致或 tag 不存在时补查 digest。digest 存在但 tag 漂移或消失只产生 warning；digest 不存在或无法确认则失败。
- `floating_tags` 中的标签和无标签的 digest 引用只验证 digest 存在；经过 `[[image]]` 批准而未固定 digest 的引用只验证 tag 存在，无 tag 按 `latest` 处理。
- `private_repositories` 中的仓库在没有凭据时 warning 并跳过；有凭据后任何失败按正常错误处理。404 不被推断为私有。
- 全局与每个 registry 分别限并发；传输错误、429、5xx 重试，认证错误和 404 不重试。错误按 regclient 的结构化错误分类，不解析文本。
- 默认增量：只检查当前引用集合中相对基线新出现的引用，基线缺省为 `origin/main`。`--full` 与 `--base` 互斥。`images` 配置或批准相对基线发生变化时自动改为全量。增量为空时成功且不访问 registry。
- `--ci crow`：PR 以目标分支为基线，push 以上一 pipeline commit 为基线；基线缺失、不可解析或不是当前 head 的祖先时失败；上一 pipeline 未成功时全量；cron 和 manual 全量。其他 CI 平台按需新增 provider。
- 凭据只读取标准 Docker 配置（`DOCKER_CONFIG` 或默认位置），不负责解密或写入凭据。
- 进度逐条输出，结束后按引用排序输出诊断和汇总。

### `stacklint images list [--ref <ref>] --format json`

输出镜像使用清单（位置：Compose 文件、服务；引用），供外部工具消费。`--ref` 使用按 Git ref 加载。

### `stacklint render [target] [--no-interpolate] --format json`

输出单个 stack 的规范化模型，用于排障和 Compose 一致性校验。

## 输出与退出码

- 默认输出面向人读，按 Compose 文件分组，每条错误带修复提示（改配置，或在 `approvals.toml` 中添加或更新哪类批准）。
- `--format json` 输出结构化结果，供 CI 和其他工具消费。
- 退出码：`0` 全部通过（可以有 warning）；`1` 存在检查失败；`2` 用法错误、配置错误或无法完成检查。

## 与 Docker Compose 的一致性

stacklint 用 compose-go 代替宿主机的 Compose plugin，两者版本可能不同。为此：

- 依赖方仓库的 CI 中对每个 stack 比较 `stacklint render --format json` 与 `docker compose config --format json` 的规范化结果，出现差异时失败。这是唯一需要 Docker CLI 的环节，不属于 stacklint 本身。
- README 记录 stacklint 内置的 compose-go 版本，以及已知的不一致。

## 非功能需求

- 性能：全仓 `check` 与 `env check` 在 40 个 stack 规模下应在数秒内完成，不随插值表达式数量线性调用外部进程。
- 确定性：相同输入产生字节一致的输出，所有集合按固定规则排序。
- 安全：生成时只写目标 stack 的覆盖模板；不在输出或日志中打印凭据；registry 访问只发生在 `images digests`。
- 测试：规则与生成逻辑以表驱动测试和 fixture 仓库覆盖；registry 通过注入 fake 测试，不访问真实网络；不弱化断言。

## 迁移计划

1. 配置目录、发现、加载、渲染和 `render` 命令，并在 stacks 仓库 CI 接入 Compose 一致性校验。
2. `env`：先迁一个 stack 试点，以既有 compose-env 的测试用例为对照。dotenv 解析结果与说明内容必须一致；引号风格允许改变，差异需要记录。
3. `check`：逐条规则迁移，以 compose-checks 的测试用例为对照，`approvals.toml` 直接复用。
4. `images`：digest 检查与 `images list`。
5. stacks 仓库切换到 stacklint，删除 `compose-env`、`compose-checks`、`images-digest-check`、`ops-compose`；运行时备份工具内联 `render_runtime_compose`；镜像清单消费方改为读取 `images list` 的输出；更新设计讨论中“不依赖 Go”的相关决策。

## 待定问题

- compose-go 合并 include 和 overlay 后能否保留字段来源；不能时，卷命名与 env 来源分组需要单独读取源文件。
- regclient 能否直接读取 `DOCKER_CONFIG` 下的标准 Docker 配置，以兼容现有的私有凭据流程。
- Crow 中 digest 检查目前由固定 plugin 镜像执行，以隔离 registry 凭据与 PR 代码；切换后的 plugin 形态和信任边界需要重新设计。
- 覆盖模板中 dotenv 赋值的编码规则，以及与 Docker 实际读取行为的差异边界。
- 输出 JSON 的 schema 与版本策略。

##已实现决策（M1）

-配置文件位于仓库根 `stacklint.toml`（非隐藏目录），`repository.root`缺省为配置目录本身。
-渲染输入平铺加载：所有 `render.env_files`与模块 `ci_env_file`并列注入同一插值环境，无优先级分层。
-未插值视图使用 `LoadModelWithContext`返回合并后的 YAML字典（`map[string]any`），不解码为强类型以保留原始表达式。
-不做逐字段 diff来源追踪：卷命名扫单文件 yaml.v3节点；env来源分组通过模块 `env_group`前缀实现。
- `render --format json`输出 compose-go规范化模型，不承诺 schema稳定；一致性校验通过规范化函数与 `docker compose config --format json`深比较。
-目标匹配：目录 / Compose文件 /唯一子串片段，零或多命中报错。
- M1搭建结构化诊断骨架（规则 ID、目标绑定、修复提示）与退出码0/1/2。
