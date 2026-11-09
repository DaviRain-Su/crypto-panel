# V1 插件生命周期技术与测试契约

日期：2026-11-09。范围：`internal/plugin/**`，对应主规格第 6–9、10.2–10.7、11.1–11.2 节。商业目标、需求与架构引用 `personal-apps-v1.md` 和 `design.md`；本文件独立定义插件模块技术、任务与测试，不代表完整 V1 验收。

## 数据与公开接口

- `Package{Plugin json.RawMessage, Main string}` 使用 `plugin/main`；`Source{Kind string, Path string, Package *Package}` 使用 `kind/path/package`。inline 仅 package；folder/file 仅绝对 path；不展开 shell、URL 或相对路径。
- `DecodeJSON([]byte, any) error`：递归拒绝重复对象键、未知字段、大小写别名、尾随 JSON 和非法类型；错误不包含输入值。`ParseManifest` 共用此入口，清单最大 64 KiB。
- `ValidationError{Path,Code,Message string}`；`ValidationResult{Valid bool,PackageHash string,Summary *Manifest,Errors []ValidationError}`，JSON 为 `valid/packageHash/summary/errors`，无效时不带摘要。应用错误返回 valid=false；来源 IO 和超限返回 `*Error{Code,Message,Status}`。
- `NewManager(*Store)(*Manager,error)` 创建唯一生命周期服务，启动时恢复中断提交并清理旧候选。`Validate(Source)` 仅静态校验。`Propose(Source,operation,targetID,expectedHash,via)` 冻结候选后返回 Installation。
- Installation 仅公开 requestId、operation、pluginId、version、packageHash、expectedHash、status、via、manifest、oldManifest、createdAt、expiresAt、completedAt、code、error。清单只含声明元数据；不包含源码、来源路径或设置值。时间为 UTC `time.Time`。
- `GetInstallation/Installations/Decide/Cancel/CancelVia/Remove/Close` 保持约定签名。`Decide` 只能由宿主原生事件调用，不包含批准回调。`Installations` 只列活跃任务；终态仍可 Get 查询。
- `TryExecution(id)(func(),error)` 是按仓库路径和 id 共享的非阻塞独占 lease；重复 release 安全。固定最大活跃 lease 数，释放后删除键，不永久增长。`Manager.SetSettings`、`Manager.SetEnabled` 与 Store setter、提交、Remove 使用同一协调源。执行期间再次变更返回 `APP_BUSY/409`，不得在持有 lease 时弹批准窗口。
- `SettingStatus` 内嵌 SettingField：普通字段 `Value *string`，秘密字段仅 `Configured *bool`，使用 omitempty，永无秘密 value。`Store.DescribeSettings/SetSettings` 与 `PackageHash` 按约定公开。`SettingsWriteError{AppliedKeys,FailedKeys []string}` 仅列声明键名，Error 文案固定且无底层异常。
- `OfficialPackage(id)(Package,string,error)` 仅从两个内嵌模板读取完整源与规范化摘要，不依赖安装目录。
- `RPCMethods() map[string][]string` 返回 evm/near/solana 查询允许表的防御性副本；host.rpc 使用链注册表 family 做精确匹配，未知与跨 family 方法在 broker 前拒绝。旧 checkRPCMethod 保留兼容，只允许任一已知 family 的方法。

## 规范化与只读校验

1. 来源文件使用有界普通文件读取，拒绝符号链接、特殊文件、路径越界和非法文件名；文件树沿用 200 文件、8 MiB 总量、2 MiB 单文件。主模块 1 MiB，单文件 JSON 包 12 MiB。
2. folder 的内容只读取一次到隔离临时快照。壳状态、安装记录、KV、系统垃圾按既有规则排除，不能携入候选状态或来源。含链接或特殊文件仍拒绝。inline/file 要求 manifest.main=index.js。
3. 清单经过同一严格校验后，以 `json.Marshal(Manifest)` 加换行规范化；其余文件原字节保留。复用 DigestDir 的排序/path-NUL-size-NUL-bytes 算法，返回 sha256: 小写 64 位十六进制。
4. 所有 .js/.mjs 文件使用 moejs 的 `syntax.ParseModule`，从不创建 runtime/runner，从不执行顶层、导入或 host。语法错误只返回位置/固定文案，不复制源码。
5. Validate 删除临时快照，不预占 id。Propose 仅把成功快照移动到运行目录扫描范围之外的隐藏 pending 根；安装时复核快照清单、文件树与摘要。
6. 官方与签名的既有原始摘要、内嵌源和签名不改写。PackageHash 使用规范化摘要；可信来源检查继续走现有完整性规则。

## 状态机、有界资源与更新

- pending → installing → installed/failed；pending → rejected/cancelled/expired。pending 期限 30 分钟，队列最多 5 个，同 id 最多一个。相同 operation/hash/expectedHash 返回现有任务（跨 HTTP/ACP 也去重，保留第一次的 via）；不同候选返回 409。满额返回 QUEUE_FULL/429。校验本身最多五路并发，避免临时快照无界增长。
- Decide(true) 先获取 lease 并变为 installing，释放 Manager 状态锁后提交；期间查询可见 installing，Cancel 返回 409。忙时保留 pending。Decide(false) 仅拒绝 pending，不能撤销 installing。重复终态确认不再执行。
- 终态最多 100 条，最长 24 小时；定时清理和每次查询/提议共同落实过期。Close 取消 pending、停止清理器并等待已开始提交；重启不恢复批准。
- install 仅允许 local.<owner>.<slug>，targetID 可空但提供时必须匹配，拒绝已有目录。via 仅 local-api/acp。
- update 要求同 id、合法 expectedHash、更高 semver、旧包可读且 unsigned；提议及提交均复核来源、规范化旧 hash、版本与迁移限制。
- 同 key 且同 secret 属性保留值、凭据、KV 与 enabled。现有字段的 secret 属性变更一律拒绝；被移除的有值字段拒绝，空字段可移除。新存储额度不得低于现有 KV key+JSON value 字节占用。无迁移脚本。
- 提交使用隐藏事务目录和持久事务记录，完整准备新版与壳状态后，旧目录移动到隐藏 backup，再把新版移入活跃目录。记录持久化并 fsync 后才切换；失败恢复旧目录。启动恢复只依据合法事务记录的固定相对子路径：活跃新版完整且摘要匹配则完成，否则恢复完整旧版；未开始切换的候选绝不安装。
- Remove 与提交/执行/settings 同 lease；使同 id pending 失效，删除声明凭据，重复卸载成功。删除前先消解同 id 残留提交记录，避免重启恢复已卸载内容。CancelVia 与 Remove 还使正在校验中的旧提议失效；它们不能在取消后注册新 pending。保留 .seeded-official 行为。

## 设置与错误边界

批量写入先验证所有键和每值 <=8192 字节（总量 <=1 MiB）再写。键按字典序执行，普通值原子更新 state.json；secret 写入凭据服务，空字符串调用 Delete。凭据服务缺失或失败不回落明文。平台错误继续处理剩余键，返回准确 appliedKeys/failedKeys。元数据读取从不返回秘密值；凭据读取失败按宿主既有“未配置”行为处理。所有对外错误为固定安全文案，不拼入文件路径、源码、值或平台原始错误。

## 任务与测试规格

按以下顺序实现，每项先添加测试再实现：

1. 严格 JSON/Source/规范化：合法两文件 folder=file=inline hash；官方原 id 格式有效；未知字段/大小写别名/嵌套重复键/尾随值/null 类型拒绝；合法 .mjs 子路径保持；非法链及多个独立错误结构化。
2. 静态安全：顶层无限循环/host 调用不执行；坏语法失败；符号链接、特殊文件、主路径越界、manifest/main/tree/输入上限失败；来源壳元数据无法改变状态或伪装信任。
3. 安装任务：pending 不进 Store；允许安装冻结内容；拒绝/取消/超时清快照；同候选去重、不同候选冲突、5 个上限；返回对象修改不影响内部状态；任务 JSON 无源码/路径/设置值；重启取消旧候选。
4. 更新：同 id 更高版本保留普通值/secret/KV/enabled；同版本/降级/错误 baseline/官方或签名/非法字段迁移/KV 缩额失败；提交前 baseline 变化与快照破坏失败；切换失败和各中断阶段启动恢复旧版或完整新版。
5. 设置：安全元数据；secret 空值 Delete；非法批次零写入；8192 边界；多个普通字段并发不丢失；凭据失败报告仅键名及安全文案。
6. 协调与卸载：执行 lease 与更新/卸载/settings 冲突，释放后可重试；Remove 取消 pending、防止旧确认复活；重复删除成功；沿用并保留全部 KV coherence、官方、zip、签名与运行时安全测试。
7. RPC：官方合法查询通过；eth_sign/send/broadcast/wallet/unknown/mixed-case/跨 family 失败且 broker 未调用；RPCMethods 副本不可修改宿主策略。

至少覆盖以下边界：空源、相对源、源不存在、根链接、嵌套链接、shell 文件链接、main 指向排除状态文件、缺 main、空 JSON、尾随 scalar、重复 escape 键、队列恰满、过期后再提议、重复 Decide、Close 与 installing 并发、卸载后确认、secret 服务缺失、空旧字段移除、非空字段属性改变、损坏 KV、事务记录坏 JSON。

## 验证记录

2026-11-09 最终模块检查已完成：

- `go test ./internal/plugin`：通过，2.647s。
- `go test -race ./internal/plugin`：通过，4.687s。
- `git diff --check -- internal/plugin docs/v1-plugin-technical.md`：通过。
- 新增 25 个测试函数及其子用例，覆盖严格 JSON、三种来源、静态解析、独立资源限制、候选冻结、队列/历史有界、安装状态机、设置批次脱敏、跨 family RPC、更新迁移、执行排他、目录切换故障、立即回退失败后的恢复，以及新版源码/state/KV 损坏或缺失时的旧版恢复。既有插件、官方完整性、zip、签名与 KV coherence 测试全部保留并通过。
- 恢复检查使用真实临时目录模拟磁盘上的各提交阶段及注入 rename 失败；未做操作系统断电实验。凭据服务使用测试 Secrets 实现，新增凭据测试值在运行时随机生成。

宿主集成须复用唯一 Manager。TryExecution 只围绕 runner 构造/执行；Manager/Store 的写入、卸载、Store.Install 与 Archive.Install 自身已持有 lease，不应外层再次持有同 id 的执行 lease。DescribeSettings 在应用忙时可返回 APP_BUSY，宿主可重试元数据读取。损坏清单无法枚举声明凭据时，安全卸载返回 BROKEN_PLUGIN/409；原有 RemoveAt 手动清理入口保留。

HTTP、原生 UI、ACP 与真实 agent 验收由对应集成层负责，本模块不预测完整 V1 结果。未创建提交，未更改其他模块或主规格。
