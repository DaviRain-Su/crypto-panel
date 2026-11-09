# V1 ACP 文件生成编排技术契约

状态：独立编排模块已实现并通过本地自动化验证；原生 UI 与真实 agent 仍需端到端验收。module 为 `github.com/davirain-su/lintel`；代码限定为 `internal/agent/authoring`。商业目标、需求及架构直接引用已授权的 [V1 规格](personal-apps-v1.md) 和 [集成契约](v1-integration-technical.md)，不另行发明 ACP 协议或安装状态机。

## 1. 职责与公共接口

```go
type Profile struct {
    ID, Name, Executable string
    Args []string
}
type Request struct {
    Profile Profile
    Prompt, WorkspaceRoot, TargetID, ExpectedHash, ExistingDir string
    Catalog json.RawMessage
    Examples []plugin.Package
    ConfigValues map[string]string
    OnUpdate func(acp.Update)
    RequestPermission func(context.Context, acp.PermissionRequest) (string, error)
}
type Result struct {
    Installation plugin.Installation
    Workspace string
    Session acp.Session
    Initialization acp.Initialization
}
func New(manager *plugin.Manager) *Service
func (*Service) Run(context.Context, Request) (Result, error)
func (*Service) Cancel()
func (*Service) Check(context.Context, Profile) (acp.Initialization, error)
func (*Service) Discover(context.Context, Profile, string) (acp.Initialization, acp.Session, error)
```

Discover 的 string 参数为 workspaceRoot；它与 Check 共用 120 秒总期限和 Run 的 busy 位，只启动、Initialize、NewSession、Close，清理临时目录并返回实际广告的 ConfigOptions，不发 prompt。返回的 session 仅用于能力展示，已关闭而不可继续使用。Request.ConfigValues 只包含用户显式选择的 configId → value（最多 32 项，每项字符串最多 4096 字节）；Run 的新会话创建后按 configId 排序逐项调用 ACP SetConfigOption，由当前会话实际广告值验证，未知项直接失败且不发 prompt，不推断模型 / 模式默认值。

原生 UI 持有一个 Service 并传入与 HTTP、UI 共用的 Manager。每次调用构建独立 Request，Run 期间不修改其中切片、映射和 JSON 的底层数据。Run 同步等待完成；调用方应在后台运行，再将 OnUpdate 和工具许可请求派发至原生主线程。回调必须及时返回，并在取消上下文后关闭许可窗口；回调错误及空选择都按拒绝处理。返回的 optionId 保留 ACP 原值，绝不根据名称或顺序改写。

同一个 Service 的 Run、Check、Discover 共用一个即时占用位：已有任务时返回 ErrBusy，不排队。Cancel 幂等且不阻塞，只取消当前活动工作；Run 返回后待确认任务属于 Manager / 原生 UI，后续撤销使用 Manager.Cancel。编排层可调用的生命周期接口只有 Validate、Propose、Cancel，没有 Decide。

Run 在入口创建覆盖准备、握手、会话、首次 prompt、所有修复及交接的同一个最长 15 分钟上下文；调用方更早的 deadline 优先。Check 最长 120 秒，只创建临时工作目录、启动、Initialize、Close，不创建 session，也不发 prompt。取消和期限由现有 ACP client 发送 session/cancel 并清理所拥有的进程；协议规定的最多 10 秒清理宽限不算新一轮生成。Manager 的同步磁盘校验 / 提议没有 context 参数，调用前后均检查总期限，超时结果不被接受；若提议期间取消，立即撤销其 pending，不返回成功。

## 2. 输入与工作目录

Prompt 必须为非空 UTF-8 文本，最多 64 KiB。WorkspaceRoot 可为空，此时使用系统临时目录；非空时必须是本机绝对规范目录路径。每次 Run 在其下创建不可预测的 `lintel-authoring-*` 目录，模式 0700，从不使用调用方目录本身作为候选。

```text
workspace/
  app/                          # 唯一产物目录，0700
    plugin.json
    index.js                    # 或清单 main 指定的合法 .js/.mjs 路径
    ...                         # 其他代码与资源文件
  context/                      # 0500；提示词明确禁止修改
    catalog.json                # 0400
    example-1.cryptoapp.json     # 0400，调用方提供的官方模板
    ...
```

Catalog 必须是有效 JSON 对象，最多 4 MiB。Examples 为调用方从内嵌官方来源获取的 1 至 8 个 Package；每个 Plugin 为 JSON 对象、最多 64 KiB，Main 最多 1 MiB，上下文合计最多 12 MiB。编排只写入调用方显式提供的这些上下文，不向 HTTP 请求 catalog，不读取现有应用的设置或秘密。模板文件按序号命名，清单 id 从不成为输出路径。调用方负责模板来源及 catalog 的实际能力正确性。

上下文使用只读文件 / 目录权限并在 prompt 中声明不得修改。工作目录不是 OS 沙箱；用户选择的本机程序仍有自身 OS 权限，owner 也能更改文件权限。不得将该目录隔离宣传成阻止恶意本机程序读取系统文件。

工作目录及错误时的候选保留给原生 UI 展示 / 用户修复，Result.Workspace 在目录创建后即有值。Check 的临时目录关闭后清理。工作目录后续清理由原生宿主负责。

## 3. 创建与修改

新建：ExistingDir 与 ExpectedHash 为空。TargetID 可为空，由 agent 选择 `local.personal.*`；显式 TargetID 必须使用此命名空间。共享校验成功后再次约束命名空间及显式目标一致性，不能通过换 id 绕过要求。

修改：ExistingDir 必须是用户明确选择的绝对规范目录；TargetID 必填，为合法 `local.<owner>.<slug>`；ExpectedHash 必须是 `sha256:` 加 64 位小写十六进制。只复制该目录的完整代码 / 资源树至 app，不读取其他应用，不访问 Store.Setting / Manager.Settings / 凭据服务。输入及复制后的清单 id 必须与 TargetID 一致；提高版本、旧基线真实性、来源及状态迁移均交给 Manager.Propose 统一检查。

复制前排除任意层级的 state.json、kv.json、install.json、settings.json、secrets.json、credentials.json、token.json、tokens.json、.seeded-official，以及 settings、secrets、credentials、.git、.ssh、.aws、.gnupg、.config、.claude、.codex、.lifecycle 目录 / 同名项；排除 `.env` / `.env.*`、.npmrc、.netrc、凭据扩展名 `.pem/.key/.p12/.pfx`、宿主暂存 `.install-*` / `.official-*` 和系统垃圾。排除项在读取内容前处理；清单中的 settings 声明元数据和 settings.js 等代码正常保留。编排无法识别人为嵌入任意源码或任意资源的秘密，调用方应只选择可信应用代码目录。

来源根、条目及打开后的描述符都检查类型 / 身份，拒绝符号链接、FIFO、设备等特殊文件和路径逃逸；使用 os.Root 约束文件访问。逐级固定目录句柄并复核身份，每批最多读取 64 个目录项，避免中间目录并发换成树内链接和无界目录枚举。Unix 打开文件额外使用 O_NOFOLLOW / O_NONBLOCK，防止检查后换成链接或 FIFO。复制保留字节但统一普通文件 0600、目录 0700，不复制权限、扩展属性或链接。

代码树最多 200 文件、单文件 2 MiB、合计 8 MiB；额外最多 1024 个访问条目防止纯目录膨胀。工作目录不得与 ExistingDir 包含或重叠，防止递归复制或污染原应用。超限、安全错误或取消时不启动 agent。

## 4. 启动、上下文和权限

Executable 必须能解析为已经存在的本机普通可执行文件；使用可执行文件加原样参数数组，不经过 shell。禁止将 npx / uvx / curl / wget、包安装器、通用 shell 及 env 包装器作为热路径启动命令；缺失程序返回明确错误，不做自动下载、安装或重试，不自动增加 bypass / full-access 参数。

Config.Env 仅从固定允许表读取 PATH、HOME、存在时的 USER、TMPDIR、XDG_CONFIG_HOME、XDG_DATA_HOME；HOME 使用用户原有目录，不建立临时 HOME。绝不继承整个宿主环境、Local API token 或任意 SECRET 变量。agent 登录由该本机程序自己的已有配置管理，Check 不付费调用模型。

首次 prompt 明确固定 app 路径，要求 plugin.json 与合法主模块，说明 catalog / 模板为只读参考、默认不申请 sign、秘密通过安装后的原生设置填写。明确“本机工具权限不等于应用安装或签名批准”，禁止调用本机安装 API、要求放宽校验或把模型回复当成批准。没有工具许可回调时返回空选择，保持 ACP 的 cancelled 拒绝语义；有回调时只接受原请求中存在的 optionId。

## 5. 完成、修复与交接状态

| 当前状态 | 触发与约束 | 下一步 / 副作用 |
| --- | --- | --- |
| idle | Run / Check 抢占成功 | preparing / checking |
| active | 另一调用到达 | ErrBusy，无队列、无启动 |
| preparing | 上下文及可选代码复制完成 | Initialize → NewSession |
| prompting | 文字、工具更新或静默 | 保持等待，不读取 / 验收产物 |
| prompting | prompt 错误、EOF、取消，或 stopReason 非 end_turn | 失败，绝不提议 |
| prompting | prompt 响应成功且 end_turn | Manager.Validate(app)，绝不运行 JS |
| validating | 无效，已修复少于 3 次 | 将结构化 ValidationError 反馈给同会话，再次等待真正 end_turn |
| validating | 无效，已修复 3 次 | ValidationFailure，保留工作目录与安全错误 |
| validating | 有效但目标 id / 命名空间不符 | 作为结构化校验失败反馈，修复同样计数 |
| validating | 有效且目标一致 | 关闭 peer，再调用同一 Manager.Propose(folder, operation, TargetID, ExpectedHash, "acp") |
| proposing | pending 与已验收摘要 / id 一致且总上下文有效 | 返回 Result，回到 idle |
| proposing | 期间取消或摘要变化 | Manager.Cancel 该 pending，返回错误 |

首次生成加最多 3 轮修复，共最多 4 个 prompt / 4 次 Validate；不因超限或被拒绝而继续。Validate 及 Propose 的静态解析、文件安全、规范化与冻结完全由现有 Manager 实现；编排不创建 runner、不执行模块、不读设置、不批准。Propose 会再次读取和校验实际产物，阻止校验后的恶意无效替换；返回摘要还必须与已验收摘要相同，合法但不同的替换同样撤销。

错误用稳定 sentinel（busy、请求、可执行文件、工作目录、复制安全 / 超限、未完成、校验失败、产物变化、Manager 不可用）及结构化 ValidationFailure 表达。StopError 保留停止原因字段但固定错误文案。错误不拼接文件内容、prompt、设置值或 peer 自报的原始消息。

## 6. 实施分解与测试规格

实施顺序：先固定本契约；写失败测试；实现接口、独占与取消；实现受控工作树 / 上下文；接入真实 acp.Start 与共享 Manager；单包普通 / race 测试及相关包回归；记录未验证项。不新增模块依赖或修改其他 agent 的文件。

必须覆盖的正常、边界与失败测试：

1. 成功仅返回 via=acp 的 pending，Store 无可运行应用；编排生命周期接口无 Decide。
2. 活跃 Run / Check 遇到另一任务立即 ErrBusy；Cancel 幂等，取消后可重新运行。
3. 15 分钟共用期限及更短调用方期限；握手 / prompt / 修复 / 校验后取消不交接。
4. 已写文件但 prompt 尚未返回时，没有校验和提议；EOF / 异常及所有非 end_turn 停止原因均无 pending。
5. 没有产物、非法 JS / 清单、目标被更改均只能修复 3 次；成功修复维持同一会话。
6. 复制保留嵌套主模块、辅助模块及资源；排除运行状态、设置、KV、宿主记录和凭据，凭据服务访问次数为零。
7. 根 / 条目符号链接、特殊文件、非法路径、目录重叠、200 文件 / 2 MiB / 8 MiB 上限与超限。
8. context 的路径及只读权限，环境只含允许表，原 HOME 保持；不暴露环境中的模拟 API token / SECRET。
9. 许可默认拒绝，关闭 / 错误 / 取消拒绝；选择保持不透明的原始 optionId，无自动允许。
10. 缺失可执行文件及 npx / uvx / curl / shell 启动失败，绝不运行安装命令；Check 不调用 NewSession / Prompt。
11. 验收与 Propose 间替换产生不同摘要时取消 pending；Propose 阻塞时取消也不留下可批准任务。
12. 生产默认真实 ACP client 通过本地测试二进制 fake peer 完成真实 stdio 握手、会话及交接，不连接提供商。

实际本地验证（2026-11-09，darwin/arm64，Go 1.27.3）：

- 先运行 `go test ./internal/agent/authoring`，确认尚无实现时测试按预期编译失败；随后实现，单包通过。
- `go test -race ./internal/agent/authoring` 通过，包含独占、取消、许可窗口取消、交接撤销、文件树限额和真实 stdio 假 peer。
- `go test ./internal/agent/... ./internal/acp ./internal/plugin` 通过。
- 生产默认工厂在测试中确实调用 acp.Start，使用本地测试二进制假 peer 完成握手、会话、显式配置、默认拒绝 / 原始许可 id、end_turn 后冻结；EOF 未完成与 max_tokens 均不提议。

仅运行本地假 peer / factory 和现有单元测试，未执行真实 agent 或付费 prompt。全仓库 `go test ./...`、其他平台、两种真实 agent 的版本 / 登录、原生权限窗、安装后运行和完整修改流程不在本模块自动化验收中，不据此声称 V1 完成。Run 工作目录的保留和清理由原生宿主负责；清理前应将 context 目录恢复为 0700。
