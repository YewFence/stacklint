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
- 不读进程环境参与插值，不自动加载主项目的 `.env`、`.env.secrets` 等部署文件。include 未声明 `env_file` 时允许 compose-go 原生读取模块目录的默认 `.env`，env 命令发现它会 warning 后继续；它不是批准的固定接线来源，其实际值不进入环境模板或 env 诊断。
- 不替代运维主机上的运行时工具，不调用业务系统的 API。
- 不生成 Compose 文件，不修改 Compose 行为。

## 术语

- 仓库：包含 stack 的代码仓库，由配置目录确定根目录。
- stack：一个部署单元，即部署根目录下包含主 Compose 文件的目录。
- 变体：同一个 stack 的替代部署组合，由有序 Compose 文件列表定义；一次部署只选择其中一个组合，不把不同变体当作同时部署的 stack。每个 stack 隐式具有 `base` 基础组合。
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

`stacklint.toml` 中的 `repository.root` 相对配置目录解析，缺省为配置目录本身。部署根、模块 Compose、`env.include_env_files`、变体的 stack、files、ci_env_files 使用仓库相对路径；`render.env_files` 是各 stack 内的相对文件名，模块的 ci_env_file 相对模块 Compose 目录，wiring_file 相对引用模块的项目目录。env 输出必须是 stack 内的文件名，不能是部署 `.env` 或 `.env.secrets`。未知键、错误类型、重复项均失败。

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
# 可选：除 module.wiring_file 外获准读取的 include 静态接线文件
include_env_files = []

[[env.variant]]
stack = "stacks/edge/alloy-agent"
name = "postgres"
# 首个文件必须是 stack 的主 Compose 文件；顺序就是 compose-go 的合并顺序
files = ["stacks/edge/alloy-agent/compose.yaml", "stacks/edge/alloy-agent/compose.postgres.yaml"]
output = ".env.postgres.example"
# 可选：追加进同一静态分析输入池；显式登记的文件必须存在
ci_env_files = ["stacks/edge/alloy-agent/.env.ci.postgres"]

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

- 用 compose-go loader 加载 stack；所有 `render.env_files` 与已注册模块的 `ci_env_file` 平铺注入同一项目环境，按配置顺序加载，不建立额外优先级层级。模块 CI 占位 env 无条件加载，不依赖 include 图。
- include 自带的 `env_file`（例如模块 wiring 文件）保持 Compose 原生优先级，低于项目级输入。
- 不读取进程环境，不加载项目目录下的 `.env`。
- 启用全部 profile。
- 同时提供两种视图：插值后的规范化模型，以及未插值的模型（部分规则需要读取原始插值表达式）。未插值视图通过 `LoadModelWithContext` 返回合并后的 YAML 字典，不解码为强类型以保留原始表达式。
- 提供 stack 的源文件关系，包括递归 include 图；不追踪合并后每个服务、卷、字段来自哪个文件，不做逐字段 diff。卷命名和 env 注释、顺序扫描单文件 yaml.v3 节点，env 模块分组按 `env_group` 前缀，缺省回退到 required_environment.prefix。
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

从 Compose 生成每个 stack 各组合的完整覆盖模板，基础输出缺省为 `.env.example`；每份模板独立使用，不是基础模板上的差量补充。运行默认值只在 Compose 中维护。两个命令支持 `--variant <name>` 和 `--format text|json`。

目标选择：

- 选中 stack 且不指定变体时处理其基础及全部已登记变体；`env check` 不给目标时处理全仓全部组合。
- `--variant base` 只选基础，其他名称只选已登记组合；任一选中 stack 不具有所选变体就失败，不回退基础。
- 不自动加载邻近 overlay。组合与输出名只从 `[[env.variant]]` 获取，名称 `base` 保留，同一 stack 的名称及输出文件名不得重复。

输入发现：

- 所有字段中的插值引用，包括端口、标签、卷、healthcheck 等，不限于 `environment`。
- `environment` 中无值的直接透传（映射形式 `KEY:` 和列表形式 `- KEY`）。
- `build.args` 中无值的透传，以及顶层 `configs`、`secrets` 的 `environment` 来源。
- 结束时用 compose-go 的整文件变量提取结果核对，发现集合必须覆盖它，否则失败。遇到不认识的输入形状直接失败。

源文件标记：

- `# @env VAR: 说明` 为变量添加说明，可选，可重复，相同说明去重，不同说明按源顺序保留。
- `# @env-source VAR: 提供方` 声明外部提供的输入，例如 `mise run ready`。这类输入只进入末尾的来源参考，不提供赋值入口。同名来源必须一致。
- `# @env-preamble VAR: 说明` 为一组相邻变量添加组级前导说明，按锚变量名关联，发射为该变量条目之前的独立注释段落。可重复，按源顺序保留，相同文本去重；允许空文本发射空注释行，用于多段落分隔。锚变量必须在该组合的模板中有赋值条目；锚不存在，或锚是 `@env-source` 等无赋值条目的外部提供输入时失败。模块分组 section 内的锚照常工作，前导段落落在锚所在 section 内。
- `COMPOSE_PROJECT_NAME` 自动归属于 Docker Compose，不能另行声明。
- 标记格式错误、引用不存在的变量时失败。

输出语义：

- 固定默认值输出为注释赋值，例如 `# VAR=value`，复制模板不会把默认值固化成覆盖项。
- 无条件必填输出为空赋值，并区分 `:?`（不得为空）与 `?`（允许为空）。
- 无值透传输出为注释空入口，不把“未设置”变成空字符串。
- 嵌套派生、条件分支、替代值、同字段重复引用等复杂情况保留完整原表达式和位置，不计算成固定值，也不把内层必填当成全局必填。例如 `${URL:-https://${DOMAIN:?...}}` 中的 `DOMAIN` 只在 `URL` 未设置时必填。
- 同一组合的有效字段中，同名变量有不一致的简单默认行为时生成失败。明确的无条件必填约束与直接引用或无值透传合并，以必填约束为准。不同变体之间不比较默认值；overlay 已替换的旧默认值不算第二个有效默认值。
- 按配置文件列表顺序访问，每个文件先自身，再按 include 声明顺序递归；同组内按源码首次出现顺序，同一字段变量按名称排序，模块前缀分组，来源参考放在末尾。
- 赋值编码由 stacklint 实现，写出后必须用 compose-go 的 dotenv 解析器读回并与原值完全一致，否则失败。

覆盖范围与豁免：

- 每个组合必须满足以下之一：已提交的模板与生成结果逐字节一致；或该 stack 在 `approvals.toml` 中有一条 `[[env_exemption]]`。豁免绑定整个 stack，覆盖基础及全部变体。
- 豁免绑定 stack 目录，`kind` 为以下之一：
  - `env-file-passthrough`：服务级 `env_file` 透传项目 `.env`，示例需要承载上游应用的大规模接口。前提是 Compose 中确实存在这种透传；不再存在时过期。需要填写原因。
  - `upstream`：`.env.example` 同步自上游项目。需要填写原因和上游来源。
  - `pending`：尚未迁移，或使用了暂不支持的形状，例如 extends。不需要原因。
- 豁免 stack 的任一被检查模板带有 stacklint 或原 compose-env 生成头时报错，提示删除豁免或恢复手写文件。
- 全仓检查报告不存在的豁免目标，局部检查不报告范围外的目标过期；env-file-passthrough 的前提按选中组合验证。
- `env generate` 必须显式给出目标，拒绝对豁免 stack 生成。先准备全部目标的分析结果、编码和输出安全检查；任一准备失败都不写。提交阶段逐文件原子替换，I/O 失败报告实际写入状态，不承诺跨文件事务。拒绝源 Compose、静态输入及输出的符号链接，不覆盖源输入文件。
- `env check` 只读，不写任何文件；缺失或漂移退出 1，配置、用法或无法完成分析退出 2。

多文件：

- 支持 include 短/长语法、递归 include、显式有序 overlay、YAML 锚点/merge key 和 Compose 的 `!reset` / `!override`。extends、label_file、其他未知标签和动态文件图形状明确失败。合并完全由 compose-go 完成，不复制或生成临时 Compose 文件图。
- include 的 path、project_directory、env_file 路径必须固定；不接受远端 Compose 资源，不允许部署输入切换文件图。
- 只有 `env.include_env_files` 和已注册模块 wiring_file 对应的 include 显式 env_file 可读取；不以 Git 跟踪状态推断授权。固定接线键只列提供文件，不提供赋值入口，不输出实际接线值；同名输入在不同 include 作用域来源不一致时失败。
- 接线文件的外部依赖可以借用静态占位值完成 dotenv 求值，但真实依赖仍进入模板；它们有 `@env-source` 时只列来源参考。CI 占位值不算运行默认值，也不证明部署已提供该变量。不能缩写的接线依赖保守显示接线文件引用，不输出派生值。
- env 分析复用 render 静态输入池并追加变体 ci_env_files，全部平铺加载；排除该 stack 所有生成输出，防止旧模板影响新模板。不读主项目部署环境或进程环境。
- include 默认 `.env` 存在时 warning 后继续 compose-go 原生加载，不将其中的值认作模板默认或批准的接线来源。未插值模型也可能含这类值，因此模板内容只从源码表达式、说明和变量名产生，env 诊断抑制可能携带实际值的加载错误详情。
- Compose 按文件先插值后合并。最终模型中消失但仍在合并前引用的变量单列加载阶段输入，原表达式和必填约束保留；不会因为 overlay 覆盖字段就漏掉加载要求。
- `@env` 按变量名关联，文件图内的说明不会随字段覆盖而删除；不建立注释到最终字段的来源追踪。

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

输出单个 stack 的规范化模型，用于排障。

## 输出与退出码

- 默认输出面向人读，按 Compose 文件分组，每条错误带修复提示（改配置，或在 `approvals.toml` 中添加或更新哪类批准）。
- `--format json` 输出结构化结果，供 CI 和其他工具消费。
- 退出码：`0` 全部通过（可以有 warning）；`1` 存在检查失败；`2` 用法错误、配置错误或无法完成检查。

## 与 Docker Compose 的一致性

渲染语义以内嵌的 compose-go 为准，不安装、不调用 Docker CLI。`internal/loader` 的 golden 测试对规范化模型和未插值模型做逐字节比较；compose-go 升级若改变插值、合并、规范化或 schema 输出，测试变红，显式审查后用 `go test ./internal/loader -update` 接受新行为。

定稿前实测过 stacks 仓库 38 个 stack（compose-go 2.16.1 vs Docker Compose v5.6.0）：渲染语义零分歧，差异全部在输出形态层（profile 裁剪、`$$` 重转义、顶层 `x-` 键保留、profile 资源连带裁剪），且 Docker CLI 自身的输出形态随版本漂移。

## 非功能需求

- 性能：全仓 `check` 与 `env check` 在 40 个 stack 规模下应在数秒内完成，不随插值表达式数量线性调用外部进程。
- 确定性：相同输入产生字节一致的输出，所有集合按固定规则排序。
- 安全：生成时只写目标 stack 的覆盖模板；env 生成和诊断不输出 CI、接线或隐式 include `.env` 的实际值；registry 访问只发生在 `images digests`。`render` 是配置视图输出，普通与未插值视图均可能携带环境解析值，不能把它当作凭据脱敏输出。
- 测试：规则与生成逻辑以表驱动测试和 fixture 仓库覆盖；registry 通过注入 fake 测试，不访问真实网络；不弱化断言。

## 迁移计划

1. 配置目录、发现、加载、渲染和 `render` 命令。
2. `env`：先迁一个 stack 试点，以既有 compose-env 的测试用例为对照。dotenv 解析结果与说明内容必须一致；引号风格允许改变，差异需要记录。
3. `check`：逐条规则迁移，以 compose-checks 的测试用例为对照，`approvals.toml` 直接复用。
4. `images`：digest 检查与 `images list`。
5. stacks 仓库切换到 stacklint，删除 `compose-env`、`compose-checks`、`images-digest-check`、`ops-compose`；运行时备份工具内联 `render_runtime_compose`；镜像清单消费方改为读取 `images list` 的输出；更新设计讨论中“不依赖 Go”的相关决策。

## 待定问题

无。原四项已定稿：regclient `config` 包直接支持标准 `DOCKER_CONFIG`（含 cred helper，机制已确认，凭据流程在接入时实机验证）；Crow plugin 信任边界移出 stacklint，归 stacks 仓库迁移议题；dotenv 写出的正确性由 compose-go 解析器往返校验保证；JSON 输出加 `version` 字段、1.0 前不承诺稳定。详见 M4 决策。

## 已实现决策（M1）

- 配置文件位于仓库根 `stacklint.toml`，`repository.root` 缺省为配置目录本身。
- 渲染输入平铺加载，不建立额外优先级层级。
- 未插值视图使用 `LoadModelWithContext` 返回合并后的 YAML 字典，不解码为强类型。
- 不做逐字段 diff 来源追踪；卷命名扫描单文件节点，env 模块分组按配置前缀。
- `render --format json` 输出 compose-go 规范化模型，不承诺 schema 稳定。
- 目标匹配目录、Compose 文件或唯一子串片段，零或多命中报错。

## 已实现决策（M2）

- 多文件组合、输入来源、豁免和写入契约以本文 env 章节为准；实现集中于 `internal/envtemplate`，CLI 负责目标选择、结构化诊断与写入。
- stacks 仓库只作为只读验收数据源，不修改其配置、工具入口或模板，不在 stacklint 运行代码中包含其路径或命名假设。
- 试点 stack 的变量顺序、说明、完整表达式、来源参考及 dotenv 赋值语义保持一致，生成头和引号风格允许不同；验收逐项机械比较，不只比较有效空赋值集合。
- JSON 输出包含 `results`、`diagnostics` 和 `exit_code`；诊断带 severity、rule、stack、variant、message、hint，结果带 stack、variant、output、status。JSON 版本策略仍未承诺稳定。

## 已实现决策（M3）

- `check` 渲染 base 及全部已登记变体组合，逐组合执行规则，诊断带 variant 维度；变体渲染输入池追加该变体的 `ci_env_files`（显式登记的文件必须存在）。
- 发布端口唯一性按"共享根 ∪ 其余每个部署根"分组、跨 stack 比对：冲突对必须来自不同 stack，或同 stack 同变体内的不同绑定（含同服务同端口不同 host_ip 的重复发布）。批准 slot 为（组、端口、协议），组从 bindings 的 Compose 路径派生，恰好一个非共享根。
- `check` 不支持 `--variant`，始终检查选中 stack 的全部组合。
- 插值分析提取为 `internal/interp`（compose-go template 记录型求值），镜像引用解析为 `internal/imageref`（distribution/reference），模块 include 图与 wiring 变量集为 `internal/wiring`；check 与 envtemplate 共用同一语义来源。
- 规则架构：`internal/check` 每条规则一个文件，统一 `(ruleContext) → []Diagnostic`；wiring 与模块必需环境是 stack 级规则，每 stack 在 base 组合上跑一遍，模块归属取全部组合 include 图的并集。include 路径解析遵循 compose-go：path 相对引用方 workingDir，`project_directory` 只成为被 include 文件的工作目录。
- 结构规则在未插值视图判断顶层 `name`（compose-go 会自动从目录名补名）；`env_file` 路径在未插值视图中已被解析为绝对路径。
- `approvals.toml` 校验与既有 compose-checks 对齐：未知键、缺字段、非法规则/端口/变量、目标文件不存在、重复目标均报错；已固定 digest 的镜像批准视为冗余并在过期批准检查中报错。过期批准只在批准引用的全部 Compose 文件都在本次范围内时判断。
- 卷命名扫描单文件 yaml.v3 节点（include 图内全部文件），渲染后必须有非空 `name` 且在源文件中显式写出。
- M3 验收：stacks 仓库 38 个 stack base 组合与 compose-checks 输出一致（0 诊断）；变体 overlay 不在 oracle 覆盖范围，alloy-agent postgres 变体因 `POSTGRES_DATA_SOURCE_NAMES` 只有生成输出提供值而渲染失败，迁移时需为其登记 `ci_env_files`。

## 已实现决策（M4 设计定稿）

- 按 Git ref 加载抽为通用内部包 `internal/refload`：go-git 纯 Go 读 `.git` 对象库（jj 共存仓库走其 git 后端），物化整树到临时目录，不碰工作区。接口通用，但 CLI 只在 `images digests --base` 与 `images list --ref` 暴露，不给 check/env 预设 `--ref`。
- 策略单一来源：`stacklint.toml`/`approvals.toml` 永远取当前工作区版本，ref 只决定被加载的内容。digests 增量时额外解析基线 ref 的 `[images]` 段与 `approvals.toml` 的 `[[image]]` 条目做规范化语义比较（注释/重排不触发），变化则转全量。
- 基线 ref 渲染失败 → 转全量并 warning（全量结果本身完整，不违反默认失败原则）。
- `images list` 记录 = stack + variant + Compose 文件 + 服务 + 引用，确定性排序，不去重；位置信息本身就是交付价值。
- JSON 输出带 `"version": 1`，1.0 前不承诺 schema 稳定。
- digests 逐条进度走 stderr，结构化结果走 stdout。
- regclient 凭证读取确认可行：`config` 包解析标准 `DOCKER_CONFIG` 与 cred helper。

## 已实现决策（M4 实现回填）

- `images list` / `images digests` 已实现；digests 增加 `--format text|json`（默认 text），JSON 形状为 `{version, mode, base, results, diagnostics, summary, exit_code}`，location 即 `images list` 的 Record。
- refload 增加 `Resolve`/`IsAncestor`（go-git），供 `--ci crow` 的基线可用性与祖先校验；crow 语义与既有 oracle 一致（PR 取 `refs/remotes/origin/$CI_COMMIT_TARGET_BRANCH`，push 依赖 `CI_PREV_PIPELINE_STATUS`/`CI_PREV_COMMIT_SHA`，cron/manual 全量）。
- registry 错误分类只用 regclient 结构化哨兵：`ErrNotFound`→not-found、`ErrHTTPUnauthorized`→authentication、`ErrHTTPRateLimit` 与其他 `ErrHTTPStatus`→retryable、其余（传输/超时）→retryable。regclient 不在错误中结构化携带 5xx 状态码，因此其他状态码一律按可重试处理：多试几次 4xx 无害，误判 5xx 为终态才会误报。重试次数与超时来自 `images.retries`/`images.timeout`，默认值 8/4/30s/2 与 oracle 一致；`images.timeout` 在配置加载时校验为合法 duration。
- 与 oracle 的唯一分类差异：oracle 按文本正则提取 HTTP 状态码区分 5xx 与其他 4xx，stacklint 不解析文本，放弃该区分（见上）。
- M4 验收：stacks 仓库全量 59 条引用实机验证通过；tag 漂移且 digest 存在报 warning，私有仓库无凭据 warning 跳过；`--base HEAD` 增量为空时不访问 registry；基线缺 `stacklint.toml` 或策略段变化自动转全量。

## 已实现决策（M5 设计定稿）

- 渲染语义以内嵌 compose-go 为准，分析、CI 与部署主机均不需要 Docker CLI。定稿依据为 stacks 仓库 38 个 stack 的实测（compose-go 2.16.1 vs Docker Compose v5.6.0）：渲染语义零分歧，差异全部在输出形态层（profile 裁剪、`$$` 重转义、顶层 `x-` 键保留、profile 资源连带裁剪），且 Docker CLI 自身输出形态随版本漂移（v5.6.0 `config --no-normalize` 对含 `$$` 的 healthcheck 误报错误）。
- compose-go 升级的行为回归由 `internal/loader` 的 golden 测试拦下：规范化模型与未插值模型逐字节钉死，覆盖插值默认值、必填表达式、嵌套与转义、有序 overlay 合并、profile、healthcheck、YAML 锚点/merge key、递归 include、`project_directory` 路径解析及显式 include `env_file` 与项目输入的优先级。升级变红后显式审查，以 `go test ./internal/loader -update` 接受新行为。
