# 创建和修改个人应用

Lintel 的个人应用仍是沙箱 JS 与原生页面数据。编码 agent 负责生成文件，壳负责校验、显示权限、安装、保存设置和运行。编码工具许可、应用安装许可和每笔钱包批准是三个独立决定。

## 在应用里使用 ACP Agent

1. 安装一个兼容 ACP v1 stdio 的本机适配器，并在它自己的环境中完成登录。
2. 在 Settings → 编码 Agent 选择 Claude Code、Codex 或自定义 ACP Agent，填写本机 executable 与 JSON 参数数组，保存并测试连接。
3. 测试连接会执行 initialize 与 session/new，显示 agent 身份、版本及它公布的模型 / 配置选项，不发送付费生成 prompt。选择只来自对端广告值；配置不兼容时会报错。
4. Apps → 创建应用，填短名和需求，点击开始生成。不要在需求里写私钥、Local API token 或 API key。
5. 工具若请求许可，壳会显示独立的原生工具窗口。检查路径和命令；拒绝或关闭会按取消处理。工作目录不是操作系统沙箱，本机编码程序仍具有自己的系统权限。
6. 生成只在 session/prompt 真正返回成功完成后进入静态校验，最多三轮修复。通过后成为 Apps 的待确认任务。创建页提供独立的“预览”和“审阅权限”操作：预览在禁止 EVM / NEAR 签名的 runtime 中执行冻结候选的声明页面，展示实际原生页面，不安装、不批准也不修改候选；点击审阅权限，原生窗口里确认才会安装。
7. 安装后若有设置项，先填写 API key 等字段并保存，壳随后验证首页；也可点击“验证首页”跳过可选设置。秘密字段只显示是否配置；留空保存保留旧秘密，使用清除按钮才删除。
8. 没有设置项时，安装后自动验证首页。应用设置和启动器显示等待设置、验证中、通过或失败；失败不会移除已安装应用，可以调整设置或修改应用后重试。验证使用禁止 EVM / NEAR 签名的独立 runtime，检查页面执行和结构；通过后仍需打开应用核对数据。

预设执行本机 `claude-code-acp` 和 `codex-acp`，不会自动下载或执行 npx / uvx 安装。Claude 适配器的当前上游已迁至 [agentclientprotocol/claude-agent-acp](https://github.com/agentclientprotocol/claude-agent-acp)，新版命令为 `claude-agent-acp`；若使用新版，在 executable 中改为该命令。Codex 的普通 CLI / app-server 不是 ACP server，使用 [zed-industries/codex-acp](https://github.com/zed-industries/codex-acp) 或其他兼容适配器。

取消生成会取消 ACP prompt 并回收拥有的进程；取消、拒绝、连接断开、超时以及非成功 stopReason 都不会当成完成。生成文件位于数据目录的独立 agent-workspaces 中，任务结束、取消或完成交接后删除，启动时清扫上次留下的目录。不把 Local API token 交给 agent，不把应用设置、KV 或凭据复制到修改工作目录。

应用详情中的修改应用只对 Unsigned `local.*` 应用开放。修改保持 id，严格提高版本，以开始修改时的旧摘要为基线；提交仍需要重新确认。官方示例和签名商店应用不能被这条路径覆盖。

## 外部 Agent：Skill + HTTP

仓库提供 [lintel-apps Skill](../.claude/skills/lintel-apps/SKILL.md)。把该 Skill 放入所用 agent 支持的 Skill 目录后使用；Lintel 不替外部 agent 安装或配置技能。Settings → Local API 显示监听地址和 Bearer token，默认 `http://127.0.0.1:8765`。只对回环开放，拒绝带浏览器 Origin 的请求，所有响应 no-store。

首次读取 catalog、官方模板和已安装清单。以本机构建的 catalog 为实际能力来源，不能根据示例假设未实现的链签名或 UI 组件。

| 接口 | 用途 |
| --- | --- |
| `GET /v0/catalog` | schema、host 能力、链 / RPC 查询允许表、limits 和 features |
| `GET /v0/examples/{id}` | 内嵌 Gas / Fear & Greed 模板，不读取私人应用源码 |
| `GET /v0/portfolio` | 上次读取的持仓快照 |
| `GET /v0/plugins` | 已安装应用、摘要、来源、设置元数据，以及 pending / installing 任务 |
| `POST /v0/plugins/validate` | 静态校验，不执行 JS |
| `POST /v0/plugins` | 新安装提议，202 + pending |
| `PUT /v0/plugins/{id}` | 同 id 更新提议，附 expectedHash，202 + pending |
| `GET /v0/installations/{requestId}` | 查询安装状态 |
| `POST /v0/installations/{requestId}/preview?page=overview` | 执行本机 API 自己的 pending 冻结候选，重新核对包摘要、清单和声明页面，返回页面 JSON 与执行元数据头。ACP、未知、非 pending 或被改写的候选拒绝执行；不安装、不批准，签名仍禁用 |
| `DELETE /v0/installations/{requestId}` | 取消本机 API 的 pending。ACP 任务返回 403 `INSTALLATION_ORIGIN_FORBIDDEN`，列表和查询不暴露其 requestId |
| `PUT /v0/plugins/{id}/settings` | 部分更新未签名 `local.*` 应用的已声明设置。宿主把安装来源记在现有凭据服务 `crypto-panel-plugins` 的账户 `provenance:<id>` 里，不记在应用目录或可被改写的索引文件中。记录必须存在、可解析，并与磁盘上的包摘要和 `install.json` 一致。记录缺失、校验失败、摘要不一致，或 `install.json` 被替换成普通伪造文件、硬链接、符号链接、目录、管道或无法解析时，返回 403 `API_MUTATION_FORBIDDEN`，凭据保持不变 |
| `POST /v0/plugins/{id}/refresh` | 执行已安装应用的一个声明页面 |
| `DELETE /v0/plugins/{id}` | 卸载未签名 `local.*` 应用与宿主状态。条件与设置接口相同：必须有匹配且已确认的宿主安装记录。伪造的普通 `install.json`、硬链接或不匹配同样返回 403 `API_MUTATION_FORBIDDEN`。升级前的可解析普通 `install.json` 只会在凭据服务成功落下记录之后迁移一次；缺失、链接、目录、读错误或写入超时不会被当成未签名。没确认的应用保持 403，直到原生界面重新安装。凭据服务删不掉来源账户时，卸载仍会删掉应用目录并报告该失败 |

catalog 的 `page` schema 与原生页面一致：stat 可用 `trend: "up" | "down"` 和最多 32 个数值的 `sparkline`，section 可用 `gauge`，其 `value` 必须为 0–100；对应 `limits` 为 `sparklinePoints`、`gaugeMin` 和 `gaugeMax`。

没有 HTTP approve、钱包签名、广播或密钥接口。refresh 和候选 preview 禁用 EVM / NEAR 签名，但声明范围内的 host.fetch 和 KV 写入仍可能产生副作用，不能代替静态 validate。预览运行的是候选而不是已安装目录，状态仍是 pending；预览结果绝不构成安装批准。

原生确认安装后，`GET /v0/plugins` 可返回当前包的可选 `verification`：requestId、packageHash、page、status（waiting_settings / running / passed / failed）、固定原因 code 及完成时间 checkedAt。它与安装任务的 installed 状态分开；passed 只表示首页执行与结构检查通过。完整保存设置且没有清除秘密时，会自动验证已跟踪的安装；清除秘密使旧结果失效并等待配置，部分保存失败不会执行。验证不记录插件日志或回传页面、异常原文。记录只存在于当前进程，重启后没有该字段；需要时继续显式 refresh。

## 包格式与请求

文件夹 source：

```json
{"source":{"kind":"folder","path":"/absolute/path/to/app"}}
```

单文件 source：

```json
{"source":{"kind":"file","path":"/absolute/path/app.cryptoapp.json"}}
```

单文件包（也可以作为 inline 的 package）：

```json
{
  "plugin": {
    "id": "local.personal.my-app",
    "name": "My app",
    "version": "1.0.0",
    "main": "index.js",
    "permissions": {},
    "navigation": [{"title":"Overview","page":"overview"}]
  },
  "main": "export function page() { return { title: 'My app', sections: [] }; }"
}
```

三种 source 不可混用。新生成 id 使用 `local.<owner>.<slug>`，默认 owner 为 personal。校验结果为 valid、packageHash、summary 和 errors。HTTP / JSON 错误是 400，超限是 413；应用语义校验失败为 200 + valid:false，提议无效应用为 422。未知 / 重复键、尾随 JSON、路径逃逸、符号链接、特殊文件及非法模块语法均拒绝。

安装返回 requestId、operation、pluginId、version、packageHash、status、createdAt、expiresAt 和 statusUrl；Location 也指向查询地址。202 只表示待确认。最多五个活跃任务，同 id 最多一个，pending 三十分钟过期。终态包括 installed、rejected、cancelled、expired、failed。关闭 Local API 或轮换 token 撤销该来源未批准任务；ACP 任务独立。重启不恢复未批准任务，旧 id 可能 404，应核对已安装摘要。

更新请求在 source 外加 `expectedHash`。必须严格提高版本，拒绝同版本改内容、降级、摘要冲突、删除非空设置、改变 secret 属性或违反新 KV 配额。保留启用状态、设置和 KV；提交失败保留旧版，崩溃恢复只接受完整旧版或已确认新版。

设置正文为 `{"values":{"label":"My pool"}}`。缺省键不变；普通空串设空，secret 空串清除。秘密只返回 configured，不返回 value 或掩码长度。系统凭据服务失败不会回退明文；部分写失败返回 appliedKeys / failedKeys。

安装来源放在现有凭据服务里，只是比应用目录旁的索引文件更难被「只会改文件」的进程改掉。这不是同一用户的硬边界。Linux 和 Windows 上，能写 `crypto-panel-plugins` 的同一用户进程可以伪造 `provenance:<id>`，通常没有提示。macOS 登录钥匙串的默认 ACL 下，读取已有秘密数据可能会弹窗，列出或新建条目通常不会。编码代理没有操作系统沙箱，不能把它写成「改不了钥匙串」。

新的商店安装和注册表条目不能使用 `local.*` id。目录里如果混有一条 `local.*` 条目，解析时跳过并记一条日志，其余条目照常显示；这条不会把整个目录判失败。此后新的 `local.*` 应用只能是个人目录安装，不能再带签名。这个限制之前已经签名安装的 `local.*` 应用不能再通过商店更新。

若迁移没能确认记录，来源无法确认的个人应用会在 Apps 页显示锁定。列表和确认窗都写「无法确认来源」，不根据 `install.json` 把应用标成未签名。确认窗在打开时记下应用 id、路径、包摘要和文件身份；用户点确认时会用同样的不跟随链接方式重读。与打开时的快照不一致，或安装记录是硬链接，都不会写记录，本机 API 保持 403。锁定列表先看到的包摘要如果在窗口打开前变了（例如 `index.js` 被改掉），确认直接拒绝，不为那棵改过的树写一条没用的记录。取消、关掉窗口，或第二个确认窗被拒绝时，界面上的「正在检查」会清掉。签名分支不会先于这次比对写下记录。取消是默认按钮。确认文案会写明：确认后本机 API 和编码代理都可以修改并卸载该应用，只应确认自己创建的应用。确认时的凭据读写不在界面线程上，界面先显示正在检查。确认成功后锁定列表会刷新，按钮消失，不能靠再点一次改写记录。确认失败会提示重新安装，而不是显示已对本机 API 放行。

残留：这个限制之前已经用签名装进 `local.*` 的应用，如果同一用户的进程在打开确认窗之前把 `install.json` 改写成普通文件，或者先硬链接到伪造文件再删掉外面的名字（链接数回到 1），它会以「无法确认来源」出现在锁定列表里。点确认可以把这个应用变成可编辑应用。用户的点击是唯一关口。仓库里没有另一份离线签名证据。

要再次按当时的 `install.json` 自动迁移，需要同时删掉哨兵 `.plugins-migration-attempted`、两个迁移标记 `provenance:migration-started` 和 `provenance:migration`，以及该应用自己的 `provenance:<id>` 记录。只是哨兵文件没写成功，下一次启动就和升级前伪造 `install.json` 的局面相同。

refresh 支持 `?page=overview`，只执行 navigation 中声明的页，返回原来的直接 page JSON。摘要和执行时间头仍使用 `X-Crypto-Panel-Package-Hash` 与 `X-Crypto-Panel-Evaluated-At`，并同时发送 `X-Lintel-Package-Hash` 与 `X-Lintel-Evaluated-At`。旧名称保留。时间表示执行完成时间，不是行情数据时间。一个应用同时只允许一个执行，API 冲突返回 409 APP_BUSY，原生重复刷新合并。当前页按 Settings 刷新间隔刷新，离开后不执行后台页面监控；失败保留同版本上次成功内容并显示时间。

实现与验收范围见 [V1 产品规格](personal-apps-v1.md)、[宿主 / HTTP 技术契约](v1-integration-technical.md)、[插件技术契约](v1-plugin-technical.md)、[ACP 技术契约](v1-acp-technical.md) 和 [生成任务契约](v1-authoring-technical.md)。真实 agent 验收结果与环境限制见[验收记录](v1-acceptance.md)，单元测试通过不等于两种 agent 的完整产品流程已通过。
