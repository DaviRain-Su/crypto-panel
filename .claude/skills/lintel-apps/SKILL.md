---
name: lintel-apps
description: 通过 Lintel 的本机 HTTP API 创建、校验、提议安装和修改个人加密应用；安装与钱包交易仍由用户在原生界面批准。
---

# Lintel 个人应用

用于用户明确要求在 Lintel 中创建或修改个人应用。你是编码 agent；Lintel 是运行应用、管理权限和钱包批准的原生壳。

## 连接

让用户在 Lintel 的 Settings → Local API 中启用接口，并通过私密渠道提供地址和 Bearer token。缺省地址为 `http://127.0.0.1:8765`，实际端口以 Settings 为准。只连接 `127.0.0.1`，不向远端发送 token。不要在源码、manifest、对话、截图、日志或提交中输出 token。用进程内 HTTP 请求头传 token，避免把它展开到命令行参数。不要直接读取壳的 Keychain 或修改已安装目录。

所有 HTTP 请求带 `Authorization: Bearer <token>`，写请求使用 `Content-Type: application/json`。浏览器 Origin 会被拒绝。401 时停止请求并请用户提供当前 token；不尝试绕过鉴权。

先读取：

- `GET /v0/catalog`：当前构建的 manifest/page/package schema、host 函数、链、RPC 只读方法、上限和 features。
- `GET /v0/plugins`：已安装应用、实际摘要、设置元数据及待确认任务。
- `GET /v0/examples/com.example.gas`、`GET /v0/examples/com.example.feargreed`：官方模板。

catalog 是当前能力的来源。不要根据记忆创造 host 函数、链、RPC 方法或 UI 组件。模板可以参考，但不得原 id 覆盖。接口未启用或返回 `MANAGEMENT_UNAVAILABLE` 时，不假装完成安装。

## 生成文件

新建使用 `local.personal.<slug>`，或者用户明确指定的 `local.<owner>.<slug>`。`slug` 使用小写字母、数字和连字符。创建独立工作目录，写 `plugin.json` 和 manifest.main 指定的 JS/MJS 模块，或写单文件 `app.cryptoapp.json`：

```json
{
  "plugin": {
    "id": "local.personal.my-app",
    "name": "My app",
    "version": "1.0.0",
    "main": "index.js",
    "permissions": {},
    "navigation": [{"title": "Overview", "page": "overview"}]
  },
  "main": "export function page() { return { title: 'My app', sections: [] }; }"
}
```

单文件包的 `plugin.main` 必须为 `index.js`。不要给包增加宿主状态、来源身份或批准字段。应用只返回原生页面数据；没有 DOM、HTML、CSS、文件系统、Keychain、原始 fetch、第三方交易 SDK 或 React。`page({name})` 必须同步完成，不能返回尚未完成的 Promise。

只申请实际需要的权限。网络使用 HTTPS 主机允许表，RPC 使用 catalog 中当前 family 的查询允许表。动态配置放入 settings；API key 标记 secret，安装后由用户填入。不要把实际秘密写进生成文件。每次钱包请求仍需用户批准；本机 API 不提供签名或广播入口。API refresh 的 host.fetch / KV 仍可能产生声明范围内的外部写入，不能把 refresh 当作无副作用的静态验证。

## 校验与提议

source 只能选一种：

```json
{"source":{"kind":"folder","path":"/absolute/path/to/app"}}
```

```json
{"source":{"kind":"file","path":"/absolute/path/app.cryptoapp.json"}}
```

```json
{"source":{"kind":"inline","package":{"plugin":{},"main":"..."}}}
```

1. `POST /v0/plugins/validate`，正文为 source。静态校验不会执行 JS。语义无效返回 200 + `valid:false` 和结构化 errors；先修复，不运行不可信顶层代码“验证”。自动修复最多三轮。
2. 校验通过后 `POST /v0/plugins` 提议安装。202 表示 `pending`，记录 `requestId`、`packageHash`、`Location` / `statusUrl`。
3. 告知用户到 Apps 的待确认区域审阅。你不能批准，token 不是安装许可，不存在 approve 接口。
4. 查询 `GET /v0/installations/{requestId}`。pending/installing 继续等待；不要密集轮询。只有 installed 才能报告已安装。rejected/cancelled/expired/failed 都不是成功，不自动重提以规避拒绝。
5. 用户要求取消时 `DELETE /v0/installations/{requestId}`。已经提交中的任务不能假称已撤销。

候选在提议时冻结；修改外部目录不会改变已展示的候选。想修改候选，先取消旧任务，再重新校验和提议。队列最多五条，同 id 最多一条，pending 三十分钟过期。关闭 Local API 或轮换 token 会撤销该入口未批准的任务。重启后任务可能 404，用已安装摘要核对，不凭 404 推断已安装。

## 设置与使用

`GET /v0/plugins` 中普通设置带 value；secret 只带 configured。设置接口：

```json
{"values":{"label":"My pool"}}
```

发送到 `PUT /v0/plugins/{id}/settings`。这是部分更新：缺省键不改，普通空串设空，secret 空串清除。不要用空串表示保留 secret。先核对全部键，系统凭据写入部分失败时按 appliedKeys / failedKeys 处理，不重传未授权的值。不要读取、打印或猜测秘密值。只对未签名的 `local.*` 个人应用生效，且该应用的 `install.json` 必须是可解析的普通文件。官方应用、已签名应用，以及 `install.json` 缺失、为符号链接或其他非普通文件、无法读取或无法解析的 `local.*` 应用返回 403 `API_MUTATION_FORBIDDEN`，文件和凭据保持不变。符号链接不会被跟随。

原生确认安装后，壳会对无设置应用自动检查首页；有设置时先等待保存，也可由用户在壳中点击验证。完整保存设置且没有清除秘密时会自动验证；清除秘密后旧结果失效并等待重新配置或用户手动验证，部分保存失败不会执行。读取 `GET /v0/plugins` 的可选 verification 元数据，核对 requestId 和 packageHash 与本次已批准任务一致。waiting_settings 提示填写 / 保存设置；running 等待检查结束；failed 说明已安装但运行检查未通过；passed 只证明首页执行和页面结构通过，不证明数据业务正确。不要把 installed 当成运行成功，不要为查询状态重复写设置或并发刷新。验证记录只在当前进程有效，字段缺失不表示通过。

用户同意执行后可 `POST /v0/plugins/{id}/refresh?page=overview` 检查实际页面内容；page 只允许 manifest.navigation 声明的页。响应仍是直接 page JSON，摘要与执行时间同时在 `X-Crypto-Panel-Package-Hash` / `X-Crypto-Panel-Evaluated-At` 和 `X-Lintel-Package-Hash` / `X-Lintel-Evaluated-At` 头。旧名称保留。它们不是行情时间。409 APP_BUSY 时等当前执行结束，不并发制造 runner。必要时引导用户从 Apps 打开原生应用。

## 修改已有应用

仅修改用户选定的 Unsigned `local.*` 应用，不更新 Official 或 Signed 应用。先取 `GET /v0/plugins` 中当前 `packageHash`。修改独立工作目录中的代码，保留 id，并严格提高版本。保留需要沿用的 settings key / secret 属性与 KV 配额；删除非空设置、改变 secret 属性、缩额冲突会被拒绝，不静默迁移。

校验后 `PUT /v0/plugins/{id}`，正文包含 source 和 `expectedHash`。旧摘要变化时停止、重读当前状态，不盲目覆盖。仍须原生确认。只有 installed 才报告更新成功；失败时旧版应保留。不要要求用户通过卸载重装来绕过一次拒绝。

`DELETE /v0/plugins/{id}` 会卸载应用及其宿主状态和凭据，只在用户明确要求卸载带有可读普通 `install.json` 的未签名 `local.*` 应用时调用。其他应用，以及 `install.json` 缺失、为符号链接或无法解析的 `local.*` 应用，返回 403 `API_MUTATION_FORBIDDEN`。`DELETE /v0/installations/{requestId}` 只能取消本机 API 自己的 pending 任务；ACP 任务返回 403 `INSTALLATION_ORIGIN_FORBIDDEN`。`GET /v0/plugins` 和 `GET /v0/installations/{requestId}` 不返回 ACP 任务的 requestId。不要自行安装 agent、修改钱包 / Passkey / DApp、调用签名入口、关闭安全断言或自动批准工具。
