# 创建和修改个人应用

Lintel 的个人应用仍是沙箱 JS 与原生页面数据。编码 agent 负责生成文件，壳负责校验、显示权限、安装、保存设置和运行。编码工具许可、应用安装许可和每笔钱包批准是三个独立决定。

## 在应用里使用 ACP Agent

1. 安装一个兼容 ACP v1 stdio 的本机适配器，并在它自己的环境中完成登录。
2. 在 Settings → 编码 Agent 选择 Claude Code、Codex 或自定义 ACP Agent，填写本机 executable 与 JSON 参数数组，保存并测试连接。
3. 测试连接会执行 initialize 与 session/new，显示 agent 身份、版本及它公布的模型 / 配置选项，不发送付费生成 prompt。选择只来自对端广告值；配置不兼容时会报错。
4. Apps → 创建应用，填短名和需求，点击开始生成。不要在需求里写私钥、Local API token 或 API key。
5. 工具若请求许可，壳会显示独立的原生工具窗口。检查路径和命令；拒绝或关闭会按取消处理。工作目录不是操作系统沙箱，本机编码程序仍具有自己的系统权限。
6. 生成只在 session/prompt 真正返回成功完成后进入静态校验，最多三轮修复。通过后成为 Apps 的待确认任务。点击审阅权限，原生窗口里确认才会安装。
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
| `DELETE /v0/installations/{requestId}` | 取消本机 API 的 pending。ACP 任务返回 403 `INSTALLATION_ORIGIN_FORBIDDEN`，列表和查询不暴露其 requestId |
| `PUT /v0/plugins/{id}/settings` | 部分更新未签名 `local.*` 应用的已声明设置。其他应用返回 403 `API_MUTATION_FORBIDDEN` |
| `POST /v0/plugins/{id}/refresh` | 执行已安装应用的一个声明页面 |
| `DELETE /v0/plugins/{id}` | 卸载未签名 `local.*` 应用与宿主状态。其他应用，以及安装记录不可读的应用，返回 403 `API_MUTATION_FORBIDDEN` |

没有 HTTP approve、钱包签名、广播或密钥接口。refresh 禁用 EVM / NEAR 签名，但声明范围内的 host.fetch 和 KV 写入仍可能产生副作用，不能代替静态 validate。

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

refresh 支持 `?page=overview`，只执行 navigation 中声明的页，返回原来的直接 page JSON。摘要和执行时间头仍使用 `X-Crypto-Panel-Package-Hash` 与 `X-Crypto-Panel-Evaluated-At`，并同时发送 `X-Lintel-Package-Hash` 与 `X-Lintel-Evaluated-At`。旧名称保留。时间表示执行完成时间，不是行情数据时间。一个应用同时只允许一个执行，API 冲突返回 409 APP_BUSY，原生重复刷新合并。当前页按 Settings 刷新间隔刷新，离开后不执行后台页面监控；失败保留同版本上次成功内容并显示时间。

实现与验收范围见 [V1 产品规格](personal-apps-v1.md)、[宿主 / HTTP 技术契约](v1-integration-technical.md)、[插件技术契约](v1-plugin-technical.md)、[ACP 技术契约](v1-acp-technical.md) 和 [生成任务契约](v1-authoring-technical.md)。真实 agent 验收结果与环境限制见[验收记录](v1-acceptance.md)，单元测试通过不等于两种 agent 的完整产品流程已通过。
