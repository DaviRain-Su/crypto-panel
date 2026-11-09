# 个人加密应用 V1：产品与实施规格

状态：V1 实施中，本文新增能力须经对应验收后才视为支持。日期：2026-11-09。核对基线：`main` 的 `06c46fa`，包含产品更名为 Lintel 及钱包 / DApp 安全修复；先前基线为 `7733a30`。

本文将原始需求与产品核对结果合并为实施约定，并按产品负责人的追加要求，将 **ACP 多 agent 接入列入 V1 必做范围**。原稿中“ACP 可选”的安排由本文更新；Skill、HTTP API、权限确认和钱包边界继续保留。

实施沿用本仓库 Go + MyGo 桌面壳、moejs 插件运行时和批准窗。本文与 [插件指南](plugins.md)、[商店规则](store.md)、[架构设计](design.md) 配套使用；现有行为以代码和回归测试为准，本文明确标注的新增行为是交付要求。

## 1. 产品定义与首批用户

产品是一款可以按个人需求生成和持续修改工具的原生加密工作台。用户提出自然语言需求，编码 agent 制作应用；壳统一提供原生页面、权限执行、链访问、设置和钱包批准。应用安装后长期留在 Apps 启动器中。

首批用户是愿意使用本机编码 agent 的加密用户：可以选择壳内已配置的 ACP agent，也可以继续在外部编辑器或终端中的 agent 里工作。V1 不要求用户理解清单、CAIP-2、SDK 或安装请求的每一步。

核心闭环：提出需求 → 生成文件 → 校验 → 用户确认权限 → 安装 → 配置 → 使用 → 继续修改同一个应用。

V1 的代表任务：

- 只读 Base Gas 工具，使用壳的 RPC broker。
- 从明确的数据 API 读取资金费率并整理表格。
- 查看指定池子的公开数据，将少量选择项保存在应用设置或 KV 中。

后两项必须先核实数据源是否可用、是否需要凭据及其限频；不承诺所有协议都已接入。

## 2. V1 范围

| 必须交付 | 明确边界 |
| --- | --- |
| 壳内选择 ACP agent，提出需求并查看生成进度 | 交付两种实际验证过的 agent 接入，并可配置其他 ACP agent；不承诺任意 CLI 都能直接使用 ACP |
| 外部 agent 的 Skill + HTTP 工作流 | 与 ACP 路径共用应用生命周期服务、校验和批准规则 |
| 文件夹、现有 zip、单文件 JSON 包 | 不增加 XML、HTML、React 或独立的另一套应用语义 |
| 能力目录、官方模板读取、静态校验 | 目录只公布当前运行时真正提供的能力 |
| 待安装、原生确认、结果查询、取消 | token、ACP 工具许可和 agent 自述均不等于安装许可 |
| 个人应用的受确认更新 | 同 id 保留设置、密钥、KV 和启用状态，失败保留旧版 |
| 设置写入、秘密值只写、卸载 | 不增加读取秘密值或钱包私钥的控制接口 |
| 当前应用页面定时刷新及手动刷新 | 不承诺所有应用后台监控、阈值通知或退出后的执行 |
| 原生展示积木与既有钱包隔离 | 第一批生成应用以只读信息工具为主；本次不增加交易动作协议 |

不在 V1：第三方应用市场升级、远程或多设备 agent、云端会话、通用 IDE/终端管理器、自动下载任意 agent、多个 agent 同时协作生成同一应用、ACP 会话跨 agent 无损迁移、后台任务系统、推送提醒、通用页面表单和 action 回调、新签名协议、永续下单、Hybrid Liquidity、Solana 发交易、本地媒体播放。

钱包容器、网络选择、Passkey、Intents、DApp 桥不重写。不添加 GitHub Actions，不放宽已有安全测试。

## 3. 已有底座与新增工作

| 基线已具备 | 本次必须补齐 |
| --- | --- |
| 文件夹/zip/签名商店安装及原生权限说明 | 可从 API 和 ACP 工作流提交的安装任务及状态 |
| `page({ name })`、原生积木、`host.*` 权限执行 | 可机读的实际能力目录与模板读取接口 |
| 普通设置与系统凭据存储、KV、日志、卸载 | HTTP 设置管理、秘密值配置状态、个人应用更新 |
| Gas、Fear & Greed 的官方来源和完整性验证 | 生成应用独立命名空间及清晰的个人来源说明 |
| loopback + token + Origin 拒绝，API runner 禁止钱包签名 | 新接口继续执行相同访问规则，补齐 RPC 只读方法规则 |
| 当前插件页随全局循环刷新 | 明确数据时间、失败状态、重复刷新及 UI/API 一致性 |
| 尚无本产品的编码 agent 接入层 | ACP client、agent 配置、会话生命周期与 Apps 创建入口 |

## 4. 用户路径与首次连接

### 4.1 壳内 ACP 路径

1. 用户在 Settings → Agents 添加或选择本机 agent，完成该 agent 自己的登录；壳显示连接测试结果。
2. Apps → 创建个人应用，选择就绪的 agent，输入需求。默认不申请 `sign`。
3. 壳创建独立生成工作目录，将当前 catalog、官方模板和生成约定交给 agent。
4. agent 在工作目录生成 `plugin.json` 和 `index.js`。对话区只显示文字、状态与工具进度；应用 UI 必须在安装后由壳绘制。
5. 本轮完成后，壳读取候选文件并调用共享校验服务。失败时将结构化错误交给 agent 修正；自动修复最多三轮，仍失败则保留文件和错误，等待用户处理。
6. 校验通过后，壳冻结候选内容，在 Apps 中显示原生权限卡。用户拒绝、取消或超时，均不得安装。
7. 用户允许后安装；若需要设置，显示原生设置表单。秘密输入优先在该表单填写，不通过对话传给模型。
8. 壳使用禁止签名的执行上下文验证第一页，并在 Apps 中提供“打开应用”。只有运行结果符合预期才报告验证完成。
9. 用户再次说“修改这个应用”时，使用同 id 的更新流程；不重新创建一个同名应用，不要求先卸载。

### 4.2 外部 agent 路径

1. Settings → Local API，由用户主动打开；默认地址为 `http://127.0.0.1:8765`，实际端口以设置为准。
2. Settings 明确显示地址、复制 token、轮换 token、关闭入口和 Skill 安装说明。token 留在系统凭据存储；复制出来的 token 由用户通过 agent 的私密配置机制提供，不写进仓库、提示词模板或日志。
3. 用户安装 `skills/generate-app/`，agent 读取 catalog 和两个官方模板，随后生成、校验并提议安装。
4. API 返回待确认任务及查询路径。agent 告知用户去桌面壳确认，不持续重发安装请求，也不能代替用户确认。
5. 安装后写设置、刷新并检查页面。普通设置可以由 agent 写入；秘密值可以由用户在壳中填写，或经私密渠道交给写入接口。

Skill 应包含可在支持 Agent Skills 的客户端中安装整个目录的说明及手动配置方法。不能声称所有编辑器都有同一个安装命令；不支持 Skill 的客户端可以读取同一目录的说明作为提示词。

轮换或撤销 token 后，旧 token 立即无效。连接失败须区分 API 未开启、地址错误、凭据无效、agent 未登录、适配器不支持协议等情况。

## 5. ACP 多 agent 接入约定

### 5.1 角色与参考来源

Lintel 是 **ACP client / host**；本机编码 agent 或它的适配器是 **ACP server**。ACP 管理编码会话；本机 HTTP API 管理应用能力。不得把 ACP 当成任意自定义工具 RPC，也不得声称 ACP 本身提供本产品的安装权限。

参考 [Zeron](https://github.com/zeronsh/zeron)，核对快照为 `533e89d63f6636cc83117c9f2ae1dbeafefbb93e`：

- [ACP 设计与生命周期](https://github.com/zeronsh/zeron/blob/533e89d63f6636cc83117c9f2ae1dbeafefbb93e/docs/research/acp.md)：共享接入层、协议协商、会话更新、配置项发现、完成与取消。
- [通用 ACP 接入实现](https://github.com/zeronsh/zeron/blob/533e89d63f6636cc83117c9f2ae1dbeafefbb93e/crates/harness/src/acp/mod.rs)：统一事件和子进程生命周期。
- [agent 注册与启动记录](https://github.com/zeronsh/zeron/blob/533e89d63f6636cc83117c9f2ae1dbeafefbb93e/crates/harness/src/acp_registry.rs)：agent 描述与启动配置分离、版本化适配器、用户自定义 ACP 命令。

借鉴上述分层，不移植 Rust/gpui、云同步或完整 agent 管理器；也不沿用参考实现中自动选择允许选项的权限处理。外部仓库和本地 agent 均不具备本产品的批准权。

### 5.2 V1 的兼容承诺

| 接入 | V1 要求 |
| --- | --- |
| Claude Code | 提供经过验证的 ACP 适配器配置预设 |
| Codex | 提供经过验证的 ACP 适配器配置预设 |
| 用户自定义 ACP agent | 支持指定本机可执行文件及参数，握手成功且具备生成所需能力后可使用 |
| Cursor 等其他编辑器或 CLI | 可以继续通过外部 Skill + HTTP 使用；只有其实际具备 ACP server 或适配器时，才列为 ACP 可连接对象 |

“支持”必须记录 agent 版本、适配器版本、协议版本、平台和实际验证结果。实施时确定并固定已测试的版本，不能把 Zeron 文档中的历史版本直接当作最新支持范围。V1 不内置某家模型 API 或重写 agent 推理循环。

V1 接入已由用户安装的 agent / 适配器，不在第一次聊天时运行 `npx -y`、`uvx` 或远程安装脚本。缺失依赖显示安装说明；以后若增加托管安装，另行规定来源、版本锁定、校验和显式安装操作。

### 5.3 配置与能力发现

agent 配置至少包含 `id`、`name`、`transport`、`executable`、`args` 和非秘密的版本信息；V1 的 transport 为 `stdio`。自定义启动使用可执行文件加参数数组，不经 shell 解释，不接受管道或重定向表达式。启动配置只能由用户在原生 Settings 中编辑，不能由插件或本机应用 API 写入。

登录及提供商凭据由 agent 自己管理。必要的秘密环境配置使用宿主凭据存储；界面和诊断只返回已设置状态，不记录秘密值。子进程不继承本产品的钱包凭据或 Local API token。

连接状态为 `unconfigured / checking / needs_auth / ready / incompatible / error`。模型、模式及可选项以握手和会话公布的能力为准；未公布的选项不显示，也不伪造一个“通用模型名”。不可用的能力应有明确说明，不静默降级为全权限模式。

### 5.4 协议与会话生命周期

以实现时核对的 ACP v1 官方协议为准，使用 JSON-RPC 2.0 的 stdio 通信；不得自行发明 `crypto/install` 等 ACP 标准方法。

- 生命周期为 `initialize` → `session/new` → `session/prompt`；`session/update` 用于呈现进度，`session/cancel` 用于停止。配置通过已公布的协议能力设置。
- `session/load`、模型选择和配置项更新仅在 agent 公布支持时使用；不支持恢复时新建会话并重新提供当前应用文件和必要摘要。
- 同一会话最多一个未完成的 prompt；后续用户补充排队，不能与正在运行的 prompt 重叠。
- 以 prompt 响应及其停止原因判断完成，不能因为几秒没有输出、一个工具结束或收到部分文字就宣告成功。
- 连接测试 / 握手超时为 120 秒；单次生成最长 15 分钟，超时明确标记失败或取消，不冒充生成完成。UI 保留停止入口。
- 停止时先发送取消；10 秒内未结束则终止所拥有的子进程，必要时再结束它的子进程组。不能终止用户独立启动的其他 agent。
- 切换 agent 新建会话，复用当前候选文件和用户需求，不承诺迁移另一家 agent 的内部历史。
- V1 最多一个活跃生成任务；已安装应用的正常使用不依赖 agent 持续在线。

### 5.5 产物交接与授权

ACP 路径不需要把 Local API token 放进模型上下文：agent 只负责生成工作目录里的产物，壳读取产物后调用与 HTTP 路径相同的生命周期服务。壳将校验结果、安装状态和运行结果作为普通上下文反馈；安装只能由真实的原生确认事件推进。

ACP 的文件系统、终端等 client capabilities 只公布本产品实际实现的部分。V1 不提供通用终端宿主或任意文件代理；适配器若依赖未实现的客户端能力，应明确显示不兼容。需要 agent 自身工具来生成文件的两种预设，必须在真实验收中走通该路径。

`session/request_permission` 是编码工具许可：显示原生 agent 工具确认，拒绝、关闭或取消均按拒绝处理；不自动选择 `allow_always`，不自动启用 bypass/full-access 模式。该许可不能批准应用安装、交易或读取钱包钥匙。agent 的其他问答也不能伪装成这些批准。

本机编码 agent 是用户选择运行的本机程序，**生成工作目录不是操作系统沙箱**。本规格不承诺限制恶意本机程序的所有系统行为。moejs 权限墙约束的是安装后的应用；壳必须始终守住自身提供的接口、秘密值与批准入口。

## 6. 应用格式与命名

保留现有文件夹：

```text
my-app/
  plugin.json
  index.js
```

V1 确定增加一个可读的单文件 JSON 包，建议后缀 `.cryptoapp.json`：

```json
{
  "plugin": {
    "id": "local.personal.base-gas",
    "name": "我的 Base Gas",
    "version": "1.0.0",
    "main": "index.js",
    "description": "通过壳读取 Base 当前 Gas。",
    "permissions": { "chains": ["eip155:8453"] },
    "navigation": [{ "title": "概览", "page": "overview" }]
  },
  "main": "export function page({ name }) { const price = BigInt(host.rpc('eip155:8453', 'eth_gasPrice')); return { title: '我的 Base Gas', sections: [{ stats: [{ label: 'Gas', value: price.toString() + ' wei' }] }] }; }"
}
```

包只有 `plugin` 和 `main` 两个键；`plugin` 是完整清单，`main` 是源码字符串。本包形式要求清单的 `main` 为 `index.js`，等价于对应的两文件文件夹。现有文件夹 / zip 的其他合法 `.js`、`.mjs` 主模块路径继续保留，不强制改名。

生成应用固定使用 `local.<owner>.<slug>`，各段遵循现有 id 校验；缺省 owner 为 `personal`。API 新安装和个人更新要求这一命名空间。通用格式校验仍可校验官方模板等其他合法 id；原有手动文件夹、zip、商店的合法 id 规则不改。

`com.example.gas`、`com.example.feargreed` 永不作为生成目标。单文件、文件夹和 zip 不允许提供能生效的宿主安装元数据或自行声明 Official / Signed。

## 7. 校验、规范化与包摘要

所有生成入口共用严格清单、权限、文件树和大小校验。未知键、坏的 CAIP-2、未支持的链、非法路径、链接及特殊文件、超限均须报错；不得为了生成成功忽略校验。旧文件夹 / zip 的安全规则不放宽。

校验过程不得执行插件代码，不创建会运行模块顶层的 runner，不调用网络、设置、存储或签名宿主。JS 语法检查只能使用不会执行代码的解析路径。校验成功不等于页面能够运行；运行检查在安装获准后进行。

`packageHash` 用来绑定候选内容和更新基线，不赋予信任。API 候选在临时快照中将清单以统一方式序列化，其余应用文件保持原字节，再复用文件树摘要规则计算 `sha256:<64 位十六进制>`。宿主状态、KV、安装记录和系统垃圾文件按统一规则排除；这些文件不能由包输入改变运行状态或来源。

等价的两文件文件夹与单文件包应得到相同的规范化摘要、权限和运行行为。官方及签名应用仍走各自现有的完整性规则，不为了规范化改写官方内嵌源或已签名文件。

文件夹源在提议时只读取一次并冻结成快照。确认后安装的是该快照；不得重新读取外部路径作为安装内容。快照若遭修改或提交前检查失败，终止任务并重新提议。

## 8. 安装任务与原生确认

### 8.1 状态机

| 状态 | 含义与允许转移 |
| --- | --- |
| `pending` | 已校验且快照已冻结，尚不可运行；可进入 installing、rejected、cancelled、expired |
| `installing` | 真实用户确认后，由壳持有提交锁进行安装；只能进入 installed 或 failed |
| `installed` | 活跃目录和来源记录提交完成，启动器可见；终态 |
| `rejected` | 用户拒绝或关闭原生权限卡；终态 |
| `cancelled` | 用户或 API 取消尚待确认的任务；终态 |
| `expired` | 待确认超过 30 分钟；终态 |
| `failed` | 提交或复核失败；终态，提供不含秘密值的错误码 |

HTTP、ACP 消息或模型输出都没有“批准安装”的状态变更接口。用户说“允许”也不能被解释为原生确认点击。

待确认应用不进入活跃应用列表，不注册页面和 runner；更新待确认时旧版继续工作。暂存目录必须与运行时扫描范围隔离。

### 8.2 排队、取消与重启

- 最多五个待处理任务，同一应用最多一个 pending / installing；完全相同操作、摘要及更新基线的重复提交返回已有任务，不重复弹卡。
- 同 id 的不同候选同时提议返回冲突；队列满返回 429。
- 用户在 Apps 的“待确认”区域可以查看、允许、拒绝或取消。已有弹窗不能使新的 HTTP 请求被悄悄当作批准或无限阻塞。
- API 的取消接口只作用于 pending；installing 时返回 409，不声称已撤销提交。
- 关闭 Local API 或轮换 token，取消仍为 pending 的 Local API 任务。ACP 生成停止时取消其尚未获准的安装任务；已经提交的安装不因停止 agent 而自动卸载。
- V1 可只保留进程内任务状态；终态记录最多 100 条，保留至 24 小时或进程退出。退出 / 重启使旧的待确认任务失效，清理残留快照，不自动恢复或执行批准。
- 重启后查询旧任务可以返回 404。调用方须重新读取应用列表并比较摘要；已提交的应用仍存在。重试不能仅凭旧的“已确认”文字继续安装。

### 8.3 用户确认内容

卡片由壳根据冻结清单生成，显示名称、id、版本、来源、网络主机、链、账户读取范围、存储额度、设置项及是否能请求签名。`sign: true` 必须突出显示“此应用会请求签名；每次仍需单独批准”。

更新还显示旧版 → 新版、包摘要及新增权限。来源统一标为“个人应用 · Unsigned”，并注明由本机 agent / API 提议；这是来源说明，不是官方审核或安全认证。

## 9. 个人应用更新

V1 支持更高版本的同 id 个人应用更新，通过与首次安装相同的任务状态机确认。API 不允许覆盖 Official 或 Signed 应用，既有商店更新规则保留。Skill 与 ACP 工作流不得用卸载后重装代替被拒绝的更新；用户主动卸载后另行发起的新安装仍须符合命名空间规则并完整确认，不能继承旧应用的官方或签名身份。

更新请求必须带客户端读取到的旧 `packageHash` 作为 `expectedHash`。提议前和确认提交前都复核；旧版已变化、被卸载、来源变化或损坏时返回冲突，不把用户对旧基线的批准套用到另一份内容。

- 新版本须大于已安装版本；相同版本改内容及降级返回 409。修复候选须提高版本。
- 每次更新都由用户确认，新增权限另外突出；不因 token 有效或权限未变就静默更新。
- 保留同 key、同 secret 属性的设置、密钥，保留 KV 和启用状态。新增字段初始为空。
- V1 不进行隐式设置迁移：删除 / 重命名 key、改变 secret 属性，或把存储额度降低到当前 KV 占用以下时，拒绝更新并说明原因。用户可先清空值 / 数据；仅已为空的移除字段允许清理，key 重命名仍视为移除加新增。
- 不执行插件迁移脚本。提交时暂停旧 runner，并协调设置与 KV 访问；候选代码及状态以可恢复的提交方式切换。
- 校验、确认复核、文件写入和切换失败时，旧版及其状态必须保留。崩溃恢复后只能运行完整旧版或完整已确认新版，不出现半安装目录。
- 新版第一次运行若报错，显示明确的运行失败及版本，允许 agent 再修正；这不等于文件提交失败，不能承诺回滚已经产生的网络副作用。

## 10. 本机 HTTP API 契约

### 10.1 共通规则

仅监听 `127.0.0.1`，沿用 Bearer token、拒绝浏览器 Origin、`Cache-Control: no-store`。所有新增路径，包括 catalog、模板和任务查询，都经过相同鉴权；不新增 CORS 放行。

除原有空正文 refresh、GET 和 DELETE 外，请求使用 `application/json`。未知字段、重复键、尾随 JSON、非法类型均拒绝，不能让清单或请求有歧义。时间为 UTC RFC 3339，摘要为 `sha256:` 字符串，id 和页名使用现有合法格式。

保留现有 `GET /v0/portfolio`、`GET /v0/plugins` 的 `plugins` 数组及 refresh 成功时的直接页面 JSON；只做兼容的字段补充。Portfolio 仍代表上次读取的快照，不能把响应时间当成资产数据时间。

| 方法与路径 | 语义 | 成功响应 |
| --- | --- | --- |
| `GET /v0/catalog` | 当前能力与版本、限制、模板索引 | 200 |
| `GET /v0/examples/{id}` | 读取内嵌官方模板源码，不执行 | 200 |
| `POST /v0/plugins/validate` | 只做静态校验 | 200，含 valid |
| `POST /v0/plugins` | 提议新安装 | 202，含任务 |
| `PUT /v0/plugins/{id}` | 提议个人更新 | 202，含任务 |
| `GET /v0/installations/{requestId}` | 查询任务状态 | 200 |
| `DELETE /v0/installations/{requestId}` | 取消尚待确认的任务 | 200，含 cancelled 任务 |
| `GET /v0/plugins` | 已安装应用、设置元数据和待确认摘要 | 200 |
| `PUT /v0/plugins/{id}/settings` | 写入声明过的设置 | 200 |
| `POST /v0/plugins/{id}/refresh` | 在 API 上下文中求页面 | 200，页面 JSON |
| `DELETE /v0/plugins/{id}` | 卸载 | 204 |

### 10.2 应用输入

validate、新安装和更新共用 `source`。source 严格三选一：

```json
{"source":{"kind":"folder","path":"/absolute/path/to/my-app"}}
```

```json
{"source":{"kind":"file","path":"/absolute/path/to/my-app.cryptoapp.json"}}
```

```json
{
  "source": {
    "kind": "inline",
    "package": {
      "plugin": {
        "id": "local.personal.base-gas",
        "name": "我的 Base Gas",
        "version": "1.0.0",
        "main": "index.js",
        "permissions": {"chains":["eip155:8453"]},
        "navigation": [{"title":"概览","page":"overview"}]
      },
      "main": "export function page({ name }) { return { title: '我的 Base Gas', sections: [{ text: String(host.rpc('eip155:8453', 'eth_gasPrice')) + ' wei' }] }; }"
    }
  }
}
```

路径必须是本机绝对路径，不接受 URL、shell 表达式或路径展开。错误不能把文件内容或秘密值拼进响应。API 不增加远程下载或 zip 上传；现有手动 zip 安装入口继续工作。

### 10.3 校验

`POST /v0/plugins/validate` 成功返回：

```json
{
  "valid": true,
  "packageHash": "sha256:<64 位十六进制>",
  "summary": {
    "id": "local.personal.base-gas",
    "name": "我的 Base Gas",
    "version": "1.0.0",
    "permissions": {"chains":["eip155:8453"],"sign":false},
    "settings": [],
    "navigation": [{"title":"概览","page":"overview"}]
  },
  "errors": []
}
```

合法请求包中的应用无效时仍返回 200，`valid: false`，没有可信的 `packageHash`，错误形状为 `{"path":"plugin.permissions.chains[0]","code":"UNKNOWN_CHAIN","message":"不支持该链"}`。能独立检查的错误尽量一次列出；解析错误不要求继续猜测后续内容。

请求 JSON 本身坏了返回 400；读取源失败返回合适的 404 / 422；超限返回 413。校验不预占应用 id、不持久化设置，不需要权限确认。官方模板原 id 可以通过格式校验，但仍不能作为个人安装目标。

### 10.4 安装、更新和状态查询

新安装提交 source；更新提交同样的 source，另加必填 `expectedHash`，且路径 id 必须等于清单 id。新安装遇到已存在 id 返回 409，调用方必须明确走更新。

提交成功始终立即返回 202，不等待用户点击：

```json
{
  "requestId": "<不可预测的任务 id>",
  "operation": "install",
  "pluginId": "local.personal.base-gas",
  "version": "1.0.0",
  "packageHash": "sha256:<64 位十六进制>",
  "status": "pending",
  "createdAt": "2026-11-09T00:00:00Z",
  "expiresAt": "2026-11-09T00:30:00Z",
  "statusUrl": "/v0/installations/<不可预测的任务 id>"
}
```

响应设置 `Location` 为 statusUrl。GET 查询返回相同身份信息和最新状态；终态可补充 `completedAt` 及安全的错误码 / 文案，不包含源码、路径内容、token 或设置值。更新的 operation 为 `update`，同时带 expectedHash。

agent 在活跃等待时查询间隔不短于两秒，离开 pending 即按结果处理；不得用重复提交代替查询。失去任务记录时按第 8 节的重启规则核对已安装摘要。

### 10.5 应用列表与来源

`GET /v0/plugins` 保留 `{"plugins":[...]}`，每个条目补充：

- `source`：`official / unsigned / signed`，表示信任来源；现有 signed、official 字段保留原义。
- `installedVia`：可确认时为 `official / folder / zip / store / local-api / acp`；无法追溯的旧本地安装为 `unknown`，不伪造历史。
- `packageHash`：当前应用内容摘要，用于个人更新；损坏且无法可信计算时缺省，保留 broken 状态。
- `settings`：声明元数据和第 10.6 节的安全配置状态。
- `pendingInstallationId`：同 id 正有提议时提供；不能把候选当成已安装版本。

顶层另加 `installations` 数组，仅列 pending / installing 的安全任务摘要，供 agent 发现新安装提议。新候选不放入 plugins 数组；拒绝、过期或卸载不留下可运行条目。

### 10.6 设置

请求形状为 `{"values":{"label":"我的池子","apiKey":"<经私密渠道提供的值>"}}`。values 是部分更新：缺省键保持原值；普通空字符串设为空；secret 空字符串明确清除已存凭据。

所有键先完整验证，再开始写入：未声明键、非字符串、重复键或超限导致整个请求拒绝且无写入。每个值最多 8192 字节，正文最多 1 MiB。secret 写入既有系统凭据存储；不可用时明确失败，不能回落到明文文件。

多个键不承诺跨系统凭据服务的分布式事务。平台写入失败时返回 500、`code: SETTINGS_WRITE_FAILED`、`appliedKeys` 和 `failedKeys`，均只列键名；调用方按结果重试，不能报告整批已成功。成功返回完整设置元数据，例如：

```json
{
  "settings": [
    {"key":"label","label":"显示名称","secret":false,"value":"我的池子"},
    {"key":"apiKey","label":"API key","secret":true,"configured":true}
  ]
}
```

secret 条目永远没有 value，不用掩码泄露长度或前后缀。清除后 configured 为 false，`host.setting` 仍返回空字符串。普通值实际存储位置是应用的 `state.json`；秘密值不能进入该文件、全局 `settings.json`、安装记录或诊断日志。

设置 schema 仍是现有字符串字段：key、label、secret、help、link。V1 不擅自添加必填、类型、默认值、下拉选项等清单字段；agent 在安装后用设置接口写入非秘密初值。

“秘密值不回读”约束壳管理的控制接口和日志。运行中的本应用仍能通过 `host.setting` 取得其声明的凭据，并向获准主机使用；沙箱不提供对恶意脚本任意编码、输出凭据的信息流证明。生成规则必须禁止把凭据写入页面、KV、日志或源码，不能把设置脱敏宣传成任意脚本均不会泄露凭据。

### 10.7 刷新与卸载

`POST /v0/plugins/{id}/refresh` 保留空正文及直接页面 JSON 响应；缺省执行第一页，可用 `?page=overview` 指定 navigation 中声明的页面。未知页返回 400；未安装 404；禁用或无可用页面 409；执行失败沿用 502。

API 上下文同时拒绝 `host.requestSign` 和 `host.requestNearSign`，在进入批准窗之前拒绝。壳内 ACP 的安装后验证也使用这个上下文。返回页面受同一积木 schema 和 256 KiB 限制。

响应头增加 `X-Crypto-Panel-Package-Hash` 和 `X-Crypto-Panel-Evaluated-At`。后者是本次脚本执行完成时间，不保证 RPC 缓存或上游数据的时间。API 刷新不负责导航主窗口，也不声称已把返回值同步为当前 UI 页面。

`DELETE /v0/plugins/{id}` 幂等卸载，应用已不存在也返回 204；停止对应 runner，协调存储写入、删除声明凭据并使相关待确认任务失效。仍在 committing 的操作须串行化，不能出现删除后被旧任务重新安装。

允许卸载官方示例，保持 `.seeded-official` 约定，重启不能种回。API 不增加独立启用接口；继续由原生应用设置管理开关。

### 10.8 错误约定

HTTP 错误保留原来的字符串 error 字段，补充稳定 code；需要时增加只含字段位置的 details。示例：

```json
{"error":"已安装内容发生变化，请重新读取后提议更新","code":"BASE_CHANGED"}
```

| 状态码 | 使用范围 |
| --- | --- |
| 400 | 请求 JSON、字段、类型或页名错误 |
| 401 / 403 | token 无效 / 浏览器 Origin 被拒绝 |
| 404 | 应用、模板、源路径或任务不存在 |
| 409 | id 冲突、版本冲突、基线变化、已有不同候选、任务不可取消、应用禁用或正忙 |
| 413 / 415 | 大小超限 / 不支持的请求媒体类型 |
| 422 | 提议的应用校验不通过、来源不允许或设置 schema 不符合 |
| 429 | 安装队列满；提供 Retry-After |
| 500 / 502 / 503 | 设置或提交失败 / 插件执行失败 / 所需宿主服务不可用 |

安全相关错误不得包含请求正文、token、秘密值或上游 authorization header；不要直接把不可信原始异常拼进管理接口。

## 11. Catalog 与官方模板契约

`GET /v0/catalog` 至少有以下字段，版本只描述接口形状，不预告未实现的能力：

| 字段 | 内容 |
| --- | --- |
| `apiVersion` | 固定 `v0` |
| `catalogVersion` | 初始为整数 1；不兼容结构变化时升级 |
| `shellVersion` | 当前壳的真实版本 |
| `schemas` | manifest、page、package 的 JSON Schema；严格对象有 additionalProperties: false；引用只在响应内部解析 |
| `permissions` | network、chains、read、sign、storageKb 的含义和可用取值 |
| `hostFunctions` | 每个实际 host 方法的 name、输入 / 输出 schema、所需权限、contexts 及上下文限制 |
| `rpcMethods` | 按链 family 列出的允许查询方法及参数约束，与宿主执行规则共用来源 |
| `chains` | 从当前 chains 注册表导出的 CAIP-2、名称、family；不公布未来链 |
| `limits` | 文件、页面、资源、API 及队列限制 |
| `features` | 该运行版本实际实现的安装、更新、单文件和 agent 接入能力；不能在实现之前报 true |
| `examples` | 两个官方 id、版本、GET 模板路径及模板摘要 |

已知清单字段是 id、name、version、main、description、author、minAppVersion、permissions、settings、navigation，细节沿用 [插件指南](plugins.md)。页面 schema 覆盖 stats、table、cards、links、text、divider、chart 的实际字段与限制。

V1 的 host 清单如下，不能把旧设计稿中尚未实现的 home、scheduled、host.prices 等写成可用：

| 方法 | 权限 / 条件 | contexts |
| --- | --- | --- |
| `host.setting` | 声明过的设置 key | ui、apiRefresh |
| `host.kv.get/put/delete` | 现有 storageKb 规则；put / delete 为写入 | ui、apiRefresh |
| `host.fetch` | 声明的网络主机、重定向及请求限制 | ui、apiRefresh |
| `host.rpc` | 声明的链与允许的查询方法 | ui、apiRefresh |
| `host.accounts` | read: balances；只返回地址信息 | ui、apiRefresh |
| `host.requestSign` | sign + 对应 EVM 链 + 原生交易批准 | ui |
| `host.requestNearSign` | sign + 对应 NEAR 链 + 原生交易批准 | ui |
| `host.log` | 有界日志，不含秘密值 | ui、apiRefresh |
| `host.i18n` | 壳的已有翻译规则 | ui、apiRefresh |
| `host.render` | 同一页面校验规则 | ui、apiRefresh |

能力出现在目录不代表当前应用已获授权。未知宿主方法在运行时不可用；不能仅靠 Skill 声称 agent 不会使用目录之外的能力。

`GET /v0/examples/{id}` 只接受 `com.example.gas` 和 `com.example.feargreed`，返回 `{"id":"...","package":{"plugin":{},"main":"..."},"packageHash":"sha256:..."}`，其中 plugin 和 main 必须是当前版本内嵌模板的完整内容；此处形状中的空对象仅表示省略。来源为内嵌副本，不读取可能被用户改坏或卸载的安装目录，不返回私人应用源码或设置。

### 11.1 资源上限

下表中的 KiB / MiB 均为 1024 进制，catalog 使用字节数字，不能只返回自然语言。

| 项目 | V1 限制 |
| --- | --- |
| 清单 / 主模块 | 64 KiB / 1 MiB |
| 文件树 | 200 个文件、总计 8 MiB、单文件 2 MiB |
| API 应用请求正文 / 单文件包输入 | 12 MiB；解包后仍检查全部独立限制 |
| 页面 | 256 KiB、32 个 sections |
| stats / table / cards / links / chart | 每 section 最多 12 个 stats、8 列 × 200 行、32 张卡、16 个链接；每图 60 点 |
| settings / navigation / KV | 20 个设置、8 个导航页、额度最多 1024 KiB；单 KV 值沿用 64 KiB |
| 单次执行 / JS 内存 | 30 秒 / 256 MiB；保留既有批准等待暂停 watchdog 的行为 |
| fetch | 30 秒、响应最多 8 MiB；方法和安全 header 继续受控 |
| pending | 最多五个、同 id 一个、30 分钟过期 |
| ACP | 一个活跃生成任务、握手 120 秒、单次生成 15 分钟 |

文字长度、控制字符及双向覆盖字符限制全部保留，以页面和清单 schema 的实际规则为准。

### 11.2 RPC 只读规则

最新基线的 `checkRPCMethod` 已补充 NEAR 广播方法的黑名单，但黑名单仍不能证明所有调用只读，`eth_sign` 等方法还需处理。V1 按链 family 建立明确查询允许表，未知方法缺省拒绝，并让 catalog 与 broker 检查使用同一来源；已经修复的广播拒绝规则继续保留回归覆盖。

允许表至少覆盖已有官方示例和现有合法插件查询测试；增加其他读取方法时须评审其语义、参数和链 family。任意签名、发送、广播、钱包控制方法均在发出网络请求前拒绝。链 id 格式合法也不代表某方法在该链被支持。

`host.fetch` 仍可按声明调用 POST 等方法，某些查询 API 需要 POST；所以“禁止本机钱包签名 / 链 RPC 广播”不等同于“任意网络调用没有副作用”。Skill 必须只生成用户明确要求的数据访问，不能把 HTTP 方法名称当作只读证明。

## 12. 原生页面、刷新与状态一致性

`page({ name })` 继续同步返回数据，不引入异步 hook。配置通过应用设置进行；页面内筛选、输入、排序事件和交易按钮不在本次接口承诺中。

- 打开页面时刷新；当前应用页按已有全局间隔刷新，初始 60 秒，继续遵循现有 Settings 范围。切到 Apps / Settings 或其他应用后，不继续轮询原应用。
- 关窗、隐藏、睡眠、进程退出均不保证后台监控。界面不显示“全天监控中”之类承诺。
- 壳在应用页面外侧显示上次执行成功时间、刷新状态与失败提示。这个时间不冒充市场数据时间；脚本需要时自行显示数据源时间。
- 刷新失败保留同版本上一份成功内容，并明确标记未更新 / 错误；第一次失败显示空态及重试入口，不显示“0”假装有效数据。
- 同一应用最多一个 page 执行；UI 重复刷新合并或跳过，API 遇到正在执行返回 409 / APP_BUSY，不无限排队。不同版本的旧结果不得覆盖新页面。
- UI 和 API 可以有独立 runner，但必须共享按应用协调的持久 KV 和设置视图。不得因各自缓存整份 kv.json 而互相覆盖；更新、卸载与写入须串行协调。
- API 返回页面是执行和渲染形状验证，不等于数据业务含义正确；若应用把上游错误画成 text，agent 应识别并报告，不能只看到 HTTP 200 就宣告数据已准备好。

## 13. Skill 与生成约定

实施时交付 `skills/generate-app/SKILL.md` 及必要引用，至少包含：

- 产品定位、固定命名空间、完整清单 / 同步 page 示例和本机 API 契约。
- 先取 catalog，再取两个当前官方模板；根据功能选模板，修改 id、名称、逻辑和最低必要权限。
- 默认 sign 为 false，不为未来需求声明宽泛主机或链；申请签名须来自用户明确需求，且 API 仍无法验证签名流程。
- 不编写目录不存在的宿主方法，不在 moejs 安装 SDK，不通过 HTML / DOM / React 绕过原生积木。
- 生成 → validate → propose → 等原生确认 → 配置 → refresh 检查；拒绝、取消、过期即停止相应操作。
- 修改已安装应用时读取当前摘要、提高版本、带 expectedHash 提议更新；不能通过换 id、卸载或覆盖文件逃避确认。
- 凭据用 settings + secret，不进入源码、页面、KV、日志或文档。错误列表用于修复，不用于要求用户放宽安全规则。
- ACP 与外部 HTTP 的入口差异、连接失败诊断，以及实际客户端的 Skill 安装说明。

标准示例提示词：

> 做一个只读 Base Gas 应用，使用 host.rpc，不申请签名。显示当前 Gas 和最新区块，把它装进我的 Apps；安装前让我确认权限。

修改示例：

> 给这个应用增加 Ethereum 对比，保留现有设置，更新前把新增的链权限列出来让我确认。

## 14. 实施顺序与代码边界

按依赖推进，不并行发明两套生命周期：

1. 共享模型、catalog / 模板、RPC 查询允许表及静态校验。
2. 冻结包、安装任务、原生权限卡、状态查询、取消与重启失效。
3. 设置 API、来源信息、卸载和个人应用受确认更新；协调 KV、runner 与提交。
4. 单文件 / 文件夹等价性，完成外部 Skill + HTTP 全流程。
5. ACP client、两个预设与自定义配置、Apps 创建 / 修改入口；复用前四步服务。
6. 刷新时间、错误保留、重复执行控制及全流程验收；更新实际使用文档。

前四步可以作为内部里程碑，不能据此宣称整个 V1 已交付；V1 完成须包括 ACP 多 agent 验收。

主要改动集中在 `internal/agent/`、`internal/plugin/`、`internal/app/` 的 Apps / Settings / 安装和 agent 会话相关代码、`internal/pages/` 的必要壳侧状态展示、文档与 Skill。允许新增独立的 `internal/acp/` 接入层；不能把 agent 进程运行能力加到插件 host。

交付时更新 README、docs/plugins.md，必要时同步 docs/design.md，将已实现能力与后续规划分开。提交信息使用英文；不把实际 token 或凭据放进提交、测试夹具、录屏或截图。

## 15. 验收与测试矩阵

安全失败测试采用现有恶意插件、未批准不签名的风格。凭据与 token 的测试值运行时生成，以变量进行断言，不把真实秘密值写成明文夹具。

| 编号 | 必须证明的结果 |
| --- | --- |
| A01 | 关闭 Local API 时无监听；开启后仅 loopback，新增每条路由均拒绝错误 token 和浏览器 Origin |
| A02 | catalog 与实际 host、链、RPC 允许表、schema 及限制一致，没有未来能力；两个模板可从内嵌源读取 |
| A03 | 官方模板及合法个人应用校验成功；未知键、坏链、语法错误、重复 JSON 键、链接、路径越界和超限明确失败 |
| A04 | 校验不执行顶层脚本、不调用网络 / KV / 设置 / 批准、不留下活跃应用 |
| A05 | folder / file / inline 候选共享校验，等价两文件内容得到相同规范化摘要与页面行为；旧 zip / 文件夹安装继续通过 |
| A06 | 提议后 pending 不可运行；真实确认后安装且可打开第一页；拒绝、关闭、取消、过期均无可运行残留 |
| A07 | 提议后修改外部文件，不会改变已展示候选；破坏暂存快照使提交失败；确认绑定包摘要 |
| A08 | 重复提议去重；同 id 不同候选冲突；队列满有界失败；重启、关闭 API 和轮换 token 不恢复待确认批准 |
| A09 | 新应用显示个人 / Unsigned，不能声明 Official / Signed；官方示例仍为 Official，篡改后损坏 |
| A10 | secret 写入后管理 GET、响应、state.json、settings.json 和诊断不含秘密值；清除和凭据服务失败有明确结果 |
| A11 | 未声明设置和非法批次无写入；平台批量写入部分失败准确返回键名，不误报全部成功 |
| A12 | 未声明主机、跨域重定向和未声明链均拒绝；权限在安装前由原生 UI 展示 |
| A13 | 有效 token 仍不能调用钱包签名或批准安装；API refresh 中两种签名宿主均在批准窗之前失败 |
| A14 | RPC 签名、发送、广播及未知方法在网络请求前失败，覆盖 eth_sign、sendRawTransaction、broadcast_tx_commit 及 family 不匹配 |
| A15 | 同 id 更高版本经确认更新，保留设置、秘密值、KV 和禁用状态；新增权限突出显示 |
| A16 | 旧基线变化、同版本改内容、降级、官方 / 签名目标、非法设置迁移、KV 缩额均拒绝；提交失败与崩溃恢复不损坏旧版 |
| A17 | 更新或卸载不会被旧 runner、旧刷新结果或 pending 任务反向覆盖；UI/API 的 KV 写入不互相丢失 |
| A18 | 卸载个人应用后消失；卸载官方示例后重启不恢复 |
| A19 | 当前页按间隔刷新，离开不继续监控；时间显示、失败保留和首次失败空态符合约定；慢请求不无限排队 |
| A20 | 两种真实 agent / 适配器各自完整走通需求 → 文件 → 校验 → 确认 → 安装 → 查看 → 同 id 修改，记录版本与平台 |
| A21 | 自定义 ACP agent 可配置和测试；缺失 executable、未登录、协议不兼容或能力不足显示明确状态 |
| A22 | ACP 假 agent 覆盖握手、分段输出、安静但未完成、重复 prompt、拒绝、取消、EOF、超时、子进程清理和非法消息 |
| A23 | ACP 工具许可拒绝不自动变允许；不启用 bypass；工具许可、对话文字和伪造完成均不能批准应用或交易 |
| A24 | 外部 agent 只靠 Skill、连接信息、catalog 和模板完成只读应用，不需要用户逐项教授协议 |
| A25 | go test ./... 通过；涉及新并发路径的包通过 race 检查；原有钱包、沙箱、官方完整性和批准安全断言不减少 |

真实 agent 测试会调用用户配置的模型服务，需在验收记录中列出实际执行与未执行项；没有凭据时标记未验收，不能用假 agent 通过替代“两种真实接入已验证”。自动化测试使用本地假 ACP peer 和可控 HTTP / RPC 服务，不把公开市场 API 的实时正确性当作单元测试前提。

## 16. V1 完成定义

交付时，用户能够在同一桌面壳中选择至少两种已验证的编码 agent，并通过统一的 ACP 工作流创建和修改自己的只读加密应用；也能继续在外部 agent 中使用 Skill + HTTP 做同一件事。

两条路径必须共用严格校验、冻结候选、用户原生确认、个人来源、设置和更新语义。应用在 agent 退出后仍能打开和使用；安装许可和每次交易批准始终由壳独立掌握。

验收记录须列出实现版本、agent / 适配器版本、自动测试结果、人工全流程结果及未覆盖项。HTTP、ACP、单文件或更新任一必做环节未完成时，标记对应里程碑完成，不标记 V1 完成。
