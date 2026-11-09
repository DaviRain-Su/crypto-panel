# V1 ACP 传输层技术与测试契约

## 依据与范围

业务、需求、架构阶段引用 `personal-apps-v1.md` 第 5 节、11.1 节及 A21–A23；本文件是独立模块的技术规格、任务拆解和测试规格。实现仅落在 `internal/acp/`，供 Apps 宿主调用，不导入 app、plugin、agent、钱包或模型 SDK；不安装 agent，不提供 MCP、文件代理、终端代理或安装批准接口。

协议核对来源为官方仓库快照 [`7441d106a853682cce61d2dd7db54123e4b9a477`](https://github.com/agentclientprotocol/agent-client-protocol/tree/7441d106a853682cce61d2dd7db54123e4b9a477)：

- `schema/v1/schema.json`：initialize、session/new、session/prompt、session/update、session/cancel、session/request_permission、session/set_config_option 的字段及枚举。
- `docs/protocol/v1/transports.mdx`：UTF-8、每条 JSON-RPC 2.0 消息以 LF 分隔；不使用 LSP Content-Length。
- `docs/protocol/v1/initialization.mdx`：协商版本 1；文本 prompt 与基础会话方法为基线；文件与终端能力只有实际实现后才能公布。

仅实现标准 v1 的文本会话子集。初始化保留 agentInfo、agentCapabilities、authMethods；缺省的可选能力按不支持处理。鉴权由 agent 自身处理，远端 `-32000` 通过类型化 RPCError 暴露，不推测登录状态，不调用 authenticate。

## 公开接口与数据

```go
type Config struct {
    Executable string
    Args []string
    Env []string
    Cwd string
    OnUpdate func(Update)
    RequestPermission func(context.Context, PermissionRequest) (string, error)
}

func Start(context.Context, Config) (*Client, error)
func (*Client) Initialize(context.Context) (Initialization, error)
func (*Client) NewSession(ctx context.Context, cwd string) (Session, error)
func (*Client) Prompt(ctx context.Context, sessionID, text string) (PromptResult, error)
func (*Client) Cancel(ctx context.Context, sessionID string) error
func (*Client) SetConfigOption(ctx context.Context, sessionID, configID, value string) (Session, error)
func (*Client) Close() error
func (*Client) Done() <-chan struct{}
func (*Client) Err() error
```

- `Config.Executable` 为本机可执行文件；Args 是参数数组，不经 shell。Cwd 为空或绝对路径。Env 是完整的显式环境，nil/空表示不继承父进程环境；宿主自行提供必要 PATH/HOME 等，不能传入钱包凭据或 Local API token。PATH 中的程序查找遵循 Go exec 的本机规则。模块不记录 Env、Args、stderr 或原始协议。
- Start 只启动并拥有子进程；不自动握手。Start 的 ctx 管理整个进程生命周期；Initialize/NewSession/Prompt 的 ctx 只管理各次操作。预先取消的 ctx 不启动进程。
- `Initialization.ProtocolVersion uint16` 必须等于 1；`AgentInfo *Implementation` 包含 name/title/version；`AgentCapabilities Capabilities` 提供 loadSession、promptCapabilities，并通过 Raw 保留其他已公布能力；`AuthMethods json.RawMessage` 保留非必选鉴权描述。返回数据为独立副本。
- `Session.ID string` 必须非空，`ConfigOptions []ConfigOption` 仅来自 agent 的 configOptions。`ConfigOption` 暴露 id/name/category/type/currentValue、平铺的 select 选项及分组；Raw 保留原始配置对象。不展示或发送推测出的模型、模式或配置。
- SetConfigOption 只接受该会话最近公布的 `type: select` 项及其 options 内的值，发送 `session/set_config_option`；响应返回全部新 configOptions。分组 select 同样校验；boolean 等类型不在此子集，不公布客户端 boolean 配置能力。
- `PromptResult.StopReason string` 为 end_turn、max_tokens、max_turn_requests、refusal、cancelled 之一。refusal/cancelled 是正常协议结果，不能当作生成成功；未知或缺失停止原因是协议错误。
- `Update` 包含 SessionID、Type、Text、Raw。Type 来自 sessionUpdate；Text 只提取文本内容块。Raw 仅为 update 对象的 JSON 副本，不含 RPC 外层或宿主环境，不执行、不渲染为可信 HTML、不解释成批准。所有 agent 文本及 Raw 均是不可信数据，可能含 agent 自行输出的信息，宿主不得直接写入诊断日志。
- `PermissionRequest` 提供 SessionID、ToolCall（ID/Title/Kind/Status/Raw）、Options（OptionID/Name/Kind）。Raw 同样仅是供原生 UI 展示的不可信 JSON。Permission 回调应遵守 ctx；OnUpdate 应及时返回。回调不在协议读取协程运行，不能阻塞消息解析。OnUpdate 按接收顺序异步执行；在失败/关闭时未送达的队列可丢弃，完成判断仅依赖 prompt 响应。
- Done 在连接不可用时关闭，Err 返回终止原因；Close 幂等，等待拥有的进程被回收。RPCError 暴露 Code/Message/Data，Error() 仅打印错误码以避免远端消息进入普通日志。`ErrClosed`、`ErrNotInitialized`、`ErrUnknownSession`、`ErrProtocol`、`ErrIncompatibleProtocol`、`ErrUnsupportedConfig`、`ErrCancelTimeout`、`ErrUpdateBacklog` 可用 errors.Is 判断；超时保留 context.DeadlineExceeded。

## 线协议与资源常量

| 项目 | 契约 |
| --- | --- |
| 协议版本 | 1 |
| initialize、新会话、配置设置期限 | 120 秒；调用方可缩短 |
| prompt 期限 | 实际取得生成槽位后最多 15 分钟；调用方可缩短 |
| 取消宽限期 | 10 秒；调用方取消 Cancel 的等待不解除后台清理 |
| 单条入站/出站消息 | 最大 4 MiB，含末尾 LF；越界明确失败 |
| JSON 深度 | 最大 64 层；重复键、非 UTF-8、非对象顶层不接受 |
| 会话配置条目 | 最多 32 项；每项 select 平铺后最多 128 个选值、最多 32 个分组；空数组允许 |
| 配置 ID/值 ID/分组 ID/当前值 | 各最多 128 UTF-8 字节；配置 ID/分组 ID 非空，选值和当前值允许协议中的空字符串 |
| 配置名称/选值名称/分组名称 | 各最多 256 个 Unicode 码点 |
| 配置 category/type | 分别最多 64/32 UTF-8 字节 |
| 更新队列 | 最多 128 条；宿主消费过慢则 ErrUpdateBacklog，终止连接 |
| 原生许可回调 | 同时最多 1 个；额外请求直接 cancelled |
| 子进程日志 | stderr 丢弃，不合并 stdout，不持久化 |

配置资源限制共同应用于 session/new、session/set_config_option 响应及 config_option_update；越界整条拒绝并以 ErrProtocol 终止连接，不静默截断可选值。分组选值按整项累计，不能靠拆组绕过 128 个选值上限；检查在配置交付宿主之前完成。原生 UI 仅渲染上述有界字段；Raw 是调试/兼容用的不可信 JSON，不应整体展开成原生配置控件。

请求字段与 JSON-RPC 外层严格分离：每个出站请求使用单调增长的整数 id；入站 id 区分整数、字符串及 null，必须原样回应 agent 请求。响应必须包含 jsonrpc=2.0、id 及 result/error 之一，不能混入 method。未知/迟到的合法响应忽略，不能完成其他请求。读取不依赖一次 Read 恰好是一条消息；LF 前的 EOF 为截断消息错误，无残余字节的 EOF 暴露 io.EOF。

initialize 公布 `fs.readTextFile=false`、`fs.writeTextFile=false`、`terminal=false`；session/new 始终带绝对 cwd 和空的 `mcpServers: []`；prompt 始终发送 `[{"type":"text","text":...}]`。未知 agent 请求返回 JSON-RPC -32601；非法请求参数返回 -32602，不调用宿主许可回调；未知合法通知忽略以允许扩展。

## 状态、并发与取消算法

| 当前状态 | 动作/条件 | 结果与副作用 |
| --- | --- | --- |
| started | Initialize 成功，版本为 1 | ready；重复 Initialize 返回独立缓存副本 |
| started | 握手超时、非法响应或版本不兼容 | failed；关闭管道、终止拥有的进程并回收 |
| started | NewSession/Prompt | ErrNotInitialized，无请求 |
| ready | NewSession 成功 | 注册 sessionId 和配置快照 |
| ready | Prompt 取得唯一生成槽 | active；其他 Prompt 等待；等待期间 ctx 结束则不发送 |
| active | update/片段/工具完成/安静 | 保持 active；不推断生成完成 |
| active | 匹配的 prompt 响应 | 校验 stopReason，释放槽位；下一 prompt 可运行 |
| active | Cancel 或 Prompt 超时 | 先取消原生许可，再发送 session/cancel；继续等待原 prompt 响应 |
| cancelling | 10 秒内原 prompt 响应 | 释放槽位；显式取消返回 nil，Prompt 超时仍返回原 ctx 错误 |
| cancelling | 宽限期结束 | ErrCancelTimeout；杀死拥有的进程组，所有等待者失败 |
| 任意 | Start ctx 取消或 Close | 停止接收新操作；活跃 prompt 先走取消流程，然后关闭与回收 |
| 任意 | EOF、非法外层或读取失败 | failed；所有 pending 请求获终止原因，清理进程 |

会话建立/配置响应及随后的配置通知，严格在协议读取协程内按线序更新缓存，再交付调用方，避免旧响应覆盖更新的 configOptions。写队列最多 32 条，反向请求回复洪泛超限则失败；写管道阻塞时操作期限与取消宽限期仍有效。

进程只由本模块通过 exec.Command 启动；Unix 创建独立进程组，清理时只针对该组并兜底终止直接子进程。非 Unix 回退为终止直接子进程，不宣称能回收独立后代。生成目录不是 OS 沙箱。协议读、写、更新回调相互独立；所有共享请求、会话和活跃任务状态受锁保护，不在锁内调用宿主回调。配置设置与 prompt 共用生成槽，避免配置切换与生成重叠。

工具许可缺省结果固定为 `{"outcome":{"outcome":"cancelled"}}`。仅当本会话正在 prompt、宿主明确提供回调、ctx 未取消且回调返回本次原始 options 中实际存在的 OptionID 时，才发送 selected。空值、错误、窗口关闭、取消、无效 ID、篡改回调参数均不能变成允许。不自动选择 allow_once/allow_always，也不添加 bypass/full-access 参数。工具许可与安装或交易批准不存在连接。

## 任务拆解与本地测试契约

实施顺序为：本技术契约 → 本地假进程测试及失败运行 → stdlib 实现 → 单包/race → 对照契约复核。测试通过重启 Go 测试二进制创建假 ACP peer，不启动真实 agent、不联网调用模型、不写真实凭据。覆盖至少以下场景：

| 场景 | 预期 |
| --- | --- |
| 启动、绝对 cwd、环境显式传入 | Start 不握手；父进程测试秘密不继承；缺失 executable/预取消失败 |
| 正常握手与能力发现 | 版本、agentInfo、authMethods、能力原样可读；无虚假文件/终端能力 |
| 新会话前置与绝对路径 | 未初始化、空/相对 cwd、未知 session 明确错误 |
| 分片、多条合并、较大文本 | 还原完整消息与文本，不受管道 Read 边界影响 |
| 并行请求乱序回复与 id 类型 | 按 id 匹配；字符串 1 不能冒充整数 1 |
| 安静但未完成、伪造完成文本、工具结束 | 不提前完成，只有 prompt response 结束 |
| 重复 prompt、跨会话生成、排队 ctx 取消 | 全连接只运行一个 prompt；取消的等待项不会发送 |
| 拒绝与远端鉴权错误 | refusal 保留；-32000 保留 RPCError |
| 默认工具许可/宿主允许/拒绝/错误/无效 ID/参数篡改 | 只选择真实提供且明确返回的 ID，其他 cancelled |
| 原生许可等待中取消、并发许可 | ctx 传播，取消后不批准；并发请求有界拒绝 |
| 已公布、分组选项及动态配置更新 | 只发送实际可选配置；不支持/未公布/伪造模型值本地拒绝 |
| 配置 UI 资源限制 | 32 项/128 选值/32 组及各字段长度的等于上限通过、上限加一拒绝；跨组合计；三种配置来源均在交付前拒绝越界 |
| EOF、截断、非法 JSON/UTF-8/重复键/非法外层/超限 | 明确错误，不挂起等待者，不误完成其他请求 |
| 握手超时、prompt 超时、取消不响应 | context 错误或 ErrCancelTimeout；拥有进程被回收 |
| Close/Start ctx 取消/后代持管道 | 先取消活跃 prompt；Unix 进程组清理；Close 可重复 |
| race | 并发 prompt、取消、配置与回调路径无竞态 |

## 本次验证结果

在 darwin/arm64、Go 1.27.3 下完成：

- 先运行本地假进程测试，得到预期的缺失接口编译失败；再实现客户端。
- 配置响应紧跟更新通知的回归测试先复现旧缓存覆盖，修复后通过；读取协程现在按消息顺序维护会话配置。
- UI 配置限额测试先复现 33 项/129 选值及过长字段被接受；增加共用解析限制后，上限及越界单元测试和三种协议来源的假进程测试全部通过。原顺序测试的大元数据仍保留，用于证明 Raw 元数据不会解除渲染字段的限制。
- `go test ./internal/acp -timeout 30s`：通过，1.342 秒。
- `go test -race ./internal/acp -timeout 45s`：通过，3.701 秒。
- 已覆盖最终响应后立即 EOF、阻塞 stdin 后超时清理、拥有进程组后代清理；未运行仓库全量测试。

## 验证边界

本包自动化只证明传输和本地进程生命周期；A20 两种真实 agent/适配器完整生成、版本、登录、平台兼容性及安装流程尚未验证。未调用收费模型或真实 agent。本模块不提供产品预设和原生 UI，宿主后续须进行兼容性与原生许可联调；不能据本包测试宣称整个 V1 已交付。
