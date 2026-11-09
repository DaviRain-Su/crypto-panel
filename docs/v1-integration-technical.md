# V1 宿主与 HTTP 集成技术契约

状态：实施中；基线 `06c46fa`，module `github.com/davirain-su/lintel`。产品 / 架构依据是 [V1 规格](personal-apps-v1.md) 与现有 docs/design.md。商业付费验证尚无实证，本轮按产品负责人已给出的实施授权推进，不虚构验证结果。

## 1. 模块与数据流

- internal/plugin：Source、Package、ValidationResult、Installation、Manager 与受控设置；持有冻结候选，唯一的内进程 Decide 接口，HTTP 不暴露批准。
- internal/agent：HTTP 解析、鉴权、限额、catalog 和安全响应；通过新增 Management 接口调用宿主，保持原 Data 接口兼容旧调用方。
- internal/app：Manager 生命周期、主线程 UI 状态、原生权限确认、API runner 禁止签名、Apps 创建与 agent 配置。
- internal/acp：stdio 会话、请求 / 通知、能力协商和取消；仅拥有编码子进程，无钱包或插件批准能力。

HTTP 提议 → Manager.Propose → Apps 待确认卡 → 原生 confirmInstall → Manager.Decide → 刷新启动器。ACP 生成文件 → 同一 Propose / Decide。两种来源都不能直接调用网络批准接口。

## 2. HTTP 集成接口

保留 Data 的 Portfolio() / Plugins() / Refresh(id)。可选的 Management 提供 ApplicationManager()、ApplicationsChanged(id) 与 RefreshPage(id,page)。HTTP 通过同一个 Manager 处理生命周期；catalog 与官方模板不依赖管理宿主。旧 Data 实现仍可启动只读接口；管理接口未实现时返回 503，不猜造成功。

请求上限：应用 source 12 MiB，settings 1 MiB。JSON 解码复用 plugin.DecodeJSON；拒绝未知 / 重复字段和尾随内容。错误保留 error 字符串并补 code；插件运行错误对外提供固定安全文案，UI 日志也不得直接记录包含凭据的原始请求。

catalog 的 manifest / page / package schema 为本地 JSON Schema，枚举 / 上限由代码定义；RPC 方法直接使用 plugin.RPCMethods。模板仅从嵌入源读取，不从已安装目录读。所有新增路径都使用现有 guard。

## 3. 主线程与执行

App 持有一个 Manager，与现有 Store 共享。HTTP goroutine 不直接访问 UI；runOnMain 把更新派回主线程。待确认卡只在用户点击审阅后调用原生确认，避免 HTTP 请求直接操作批准结果。Decide 的文件操作在后台执行；UI 关闭视为拒绝。

同应用 UI / API 执行共用有界 lease，运行中重复 UI 刷新跳过、API 返回 APP_BUSY。执行完成必须释放 lease。卸载、更新与设置改变停止旧 runner，递增页面 generation；旧结果不得覆盖新状态。刷新失败保留上一份同版本成功页面并显示错误及上次成功时间。

ACP 会话配置保存为非秘密 executable / args / id；提供商登录由 agent 自己管理。生成目录与活跃插件目录分离。catalog / 模板为只读上下文，不给 agent Local API token。真实 prompt 完成后才读取生成文件；取消或失败不提议安装。工具许可由原生窗口决定，默认拒绝。

## 4. 测试与实施顺序

先写失败测试再添加实现，分为：

1. HTTP 路由：有效 token 正常路径；缺失 / 无效 token、Origin、超限、坏 JSON、未知 / 重复键；安装不出现 approve 路由；旧路由仍兼容。
2. 宿主：pending 不运行、确认后打开、拒绝 / 取消无安装；API签名回调在批准前拒绝；更新保持状态；token撤销取消对应pending。
3. catalog：方法和链来自当前注册表，模板源固定，schema与当前页面边界一致。
4. ACP UI：配置 / 缺失可执行文件、未登录 / 不兼容、文本进度、取消、成功后冻结产物；假进程测试不调用付费模型。
5. 并发：UI/API重叠、KV视图、设置/更新/卸载与旧结果；有界队列与任务清理。
6. 回归 go test ./...，新并发包 race，再做原生界面和两种真实 agent 的可执行环境验收。缺少登录或凭据明确记录为未验证，不能冒充通过。

## 5. 边界清单

空 source；三种 source 混用；非法路径；官方 id；无导航页；同版本不同代码；旧摘要已变；队列已满；pending 超时；API关闭；token轮换；旧任务查询；禁用应用；运行中更新；EOF未返回prompt；无输出但prompt未结束；取消后文件仍存在；Keychain不可用；设置未声明；请求中带秘密值的错误。

代码、单包测试和变更复核完成后再更新实际使用文档，不能在尚未实现时把 README 的现状提前改为已交付。

## 6. Go 链 SDK 核对（2026-11-09）

EVM 已依赖 `ethereum/go-ethereum v1.17.10`；查询经现有 broker。Solana 基金会的 `solana-foundation/solana-go` 推荐稳定 v1，当前 v1 的 module 仍是 `github.com/gagliardetto/solana-go`；基金会 module 的 v2 为 RC。NEAR 有 Aurora 维护的 `aurora-is-near/near-api-go`，最近推送为 2025-06-23；`textileio/near-api-go` 已归档。`near/borsh-go` 是官方序列化库，不是完整链客户端。

V1 保留现有 NEAR / Solana 查询与交易实现。后续确有交易解析、指令构造等需求时，SDK 可放在壳的链适配层，继续经过固定网络、权限、原生批准与签名边界，不向 moejs 暴露完整 SDK。
