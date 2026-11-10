# 个人加密应用 V1 验收记录

核对日期：2026-11-09。平台：darwin/arm64，Go 1.27.3。远程基线：`06c46fa`；实现与本验收记录随 V1 源码一同提交。

生命周期、Local API、ACP 编排、原生创建 / 修改入口与外部 Skill 已接入，全仓回归及新增并发路径检查通过。**V1 尚未完整验收**：[产品规格](personal-apps-v1.md) 的 A20 要求两种真实 agent 各自完成创建和修改，A24 还要求外部 agent 仅凭 Skill 完成流程。

## 自动检查

| 检查 | 实际结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | 通过；原有钱包、链、DApp、Passkey、沙箱、官方完整性与批准安全测试保留 |
| `go test -race ./internal/acp ./internal/agent/... ./internal/plugin ./internal/app -count=1 -timeout=180s` | 通过；覆盖新增并发路径、取消、冻结候选、更新事务及 UI / API 执行协调 |
| `TestRefreshRebuildsRunnerAfterPackageUpdate` | 通过；包变化后重建 runtime，待确认更新不清空当前成功页面 |
| `TestNativeReviewLeavesCreatorForInstalledSettings` | 先复现失败，修复后通过；安装成功时退出创建表单并进入应用设置 |
| `go tool mygo build` | 通过；产出 `build/darwin-arm64/Lintel.app` 和 `lintel-0.1.0-arm64.dmg` |
| `git diff --check` | 通过 |

校验 / 更新测试使用真实临时目录和故障注入，系统凭据使用可控测试实现；没有做机器断电实验。协议和编排自动测试使用本地假 ACP peer，不访问真实模型服务。

## 实际原生窗口

验收实际启动了当前 Go + MyGo 应用，并通过当前依赖版本的 MyGo automation 操作其原生窗口，不以 `ui.NewTester` 代替运行。

为避免混用日常数据，验收入口通过临时 Go overlay 链接 MyGo automation，使用独立应用名 `Lintel V1 Check 349c7fe0` 与 `build/v1-check/` 下的数据目录。overlay、假 peer 二进制、socket 和截图均位于被忽略的 build 目录，不进入生产应用。生产构建未增加 automation 导入或调试端点。

已观察到：

- Apps 的创建入口可打开，短名与需求填写后生成按钮启用。
- Settings 可以选择 Claude Code / Codex / 自定义 ACP，使用 executable 和 JSON 参数数组。
- Claude 的真实连接测试显示 `@zed-industries/claude-code-acp 0.16.2`。
- 自定义配置使用已有 `TestAuthoringPeerProcess` 测试二进制，通过真实 stdio 返回 `local-test-peer 1` 及 `local-test-model` 配置项。
- 点击开始生成后出现独立的“编码工具许可”原生窗口，展示真实选项，并明确该许可不批准安装或钱包交易。
- 假 peer 完成后出现 `local.personal.stdio` 的 pending 候选；工具许可没有直接安装应用。
- 独立原生安装窗口出现后，该候选达到 installed；Apps 启动器将其标为未签名，两个内嵌示例仍标为官方。
- 个人应用可打开，页面执行成功并显示上次成功刷新时间；设置页包含修改入口。该假 peer 的最小页面为 `sections: []`，正文为空属于测试产物内容。
- 单独的原生拒绝用例达到 rejected，候选退出待确认区域，没有成为已安装应用。

窗口交互期间，部分 automation `wait` 没有及时观察到异步界面更新；随后以重新捕获的截图、快照及实际安装目录核对最终状态。弹窗关闭后旧 ref 失效，未把失效引用或等待超时命令记为成功。

关键截图保存在 `build/v1-check/`：`apps-pending.png`、`apps-create.png`、`agent-connected.png`、`tool-permission.png`、`generation-result.png`、`personal-page-final.png`。截图是本机验收产物，未提交。

## 真实适配器

| 项目 | 实际结果 |
| --- | --- |
| Claude executable | `/opt/homebrew/bin/claude-code-acp` |
| 握手返回身份 | `@zed-industries/claude-code-acp`，版本 `0.16.2`，协议版本 `1` |
| initialize → session/new | `TestInstalledACPHandshake` 与实际 Settings 连接测试通过；不发送生成 prompt |
| 配置发现 | 本机该版本返回 0 项 ConfigOptions，界面没有猜造模型列表 |
| 真实模型生成 / 安装 / 同 id 修改 | 未执行；本次完整原生生成使用假 peer |
| Codex | 本机有 `codex`，验收时未找到 `codex-acp`；普通 CLI / app-server 不算 ACP server |
| 第二种真实 agent 完整流程 | 未验收；没有自动下载或安装适配器 |

Claude 当前上游命令已更名为 `claude-agent-acp`。本机旧版仍可握手；设置中的 executable 可填写用户已安装的实际命令。预设不表示已安装、已登录或已通过全流程验收。

## 安装后运行验证补充检查（基线 879a93b）

本轮补齐安装后自动验证：无设置的已批准应用自动执行首页；有设置时先等待保存，也能手动验证可选设置；设置完整保存且没有清除秘密时验证，部分失败不执行。清除秘密后旧结果失效并等待配置或手动验证。UI 与 GET /v0/plugins 的 verification 元数据区分安装成功和运行检查通过，绑定批准的 requestId / packageHash，不包含页面或异常原文。状态只在当前进程保存。

- 新增测试先复现“安装后没有验证结果”，再验证通过 / 失败、配置等待、重试、EVM / NEAR 模块顶层与页面签名拒绝、随机秘密值的异常和日志不泄露。
- 并发测试覆盖同 id 合并、共享执行 lease、最多五个活动工作及容量回收、队列中的更新 / 卸载 / 宿主变化 / 退出、迟到结果与新生成任务隔离、设置部分写失败和禁用更新；补充测试还覆盖无导航页不执行、HTTP 完整保存通知和重启不恢复验证或自动执行。
- 复核发现 HTTP 清空秘密被当作普通保存成功并自动执行。新增回归先复现该错误，再修正设置通知：从已声明字段与本次请求识别明确清除，不读取秘密值；清除后等待配置且无自动执行，仍允许用户手动验证。该边界由 HTTP 与真实 runtime 自动测试覆盖，未另做原生秘密字段实机操作。
- 清空秘密的边界修正后，重新执行全仓 `go test ./... -count=1 -timeout=180s`、`go test -race ./internal/app ./internal/agent/... -count=1 -timeout=180s` 和生产 `go tool mygo build`，全部通过；Go 格式、变更差异与 13 处本地文档链接检查通过。
- 原生验收使用独立应用名 `Lintel Post-install Check 349c7fe0`、`build/post-install-check/` 数据目录与 Go overlay，三份本地固定测试应用，不调用真实 agent 或模型服务。
- 实际点击原生审阅与安装后，无设置应用显示“首页已通过运行检查”及检查时间；有设置应用先显示“等待保存设置后验证”，填写显示名称并保存后自动通过；故意抛错的应用保持 installed，显示失败与验证入口。
- 启动器同时显示通过 / 失败状态；从已验证应用点击打开，实际首页显示测试值 42。关键截图：`no-settings.png`、`waiting-settings.png`、`settings-passed.png`、`verification-failed.png`、`launcher-statuses.png`、`opened-page.png`，均在被忽略的 `build/post-install-check/` 中。

一次安装点击的 `--settle` 因安装窗口已关闭返回 no_window；随后读取主窗口与截图确认最终验证状态，没有把该命令失败记作通过。最初验收候选用 local-api 来源，会按既有规则在启动时关闭 API 被取消；验收入口改为独立 ACP 来源后检查原生流程，没有修改产品取消语义。

发布前已同步远程 `main` 的图标更新 `1113d5c`。重新检查时，既有 `TestRebindSamePort` 返回 `use of closed network connection`，100 次重复复现 2 次；原本先关闭 listener 再停止 HTTP 服务，会使服务重复关闭已关闭的连接。调整为先停止 HTTP 服务，再关闭可能尚未登记的 listener，保留原有端口复用断言与回环绑定。修正后端口重绑定测试连续 100 次通过，全仓测试、App / Agent race 检查与生产 MyGo 构建重新执行并全部通过。

## 2026-11-10 真实 Claude Agent 补充验收

环境：darwin/arm64，Go 1.27.3，`@agentclientprotocol/claude-agent-acp 0.102.0` 与 Claude Code `2.1.322`。实时测试通过显式 `LINTEL_TEST_ACP_EXECUTABLE=/opt/homebrew/bin/claude-agent-acp` 启动；默认全量测试不会调用模型。

首次真实握手发现新版 adapter 在 `session/new.configOptions` 中公布 868 个唯一模型值，旧解析上限 128 使整个会话以 `invalid protocol message` 失败。修复将协议解析上限与 UI 上限分离：wire 保留 4 MiB 总帧限制并允许最多 1024 个值，原生选择器最多绘制 128 个；重复值、畸形分组、1025 个值及字段上限仍拒绝。回归测试为 `TestConfigOptionsAcceptALargeModelCatalog`、`TestConfigOptionsRejectTooManyOrDuplicateValues`、`TestConfigLargeCatalogStaysWithinWireBounds` 和 `TestCodingSelectChoicesBoundsLargeModelCatalog`。默认 Claude executable 更新为 `claude-agent-acp`。

`TestInstalledACPCreatesPreviewsAndInstalls` 已通过，耗时 215.00 秒（包总耗时 218.356 秒）。真实模型创建 `local.personal.near-briefing` 1.0.0，无签名、无网络、无存储、无设置和秘密；生成后未安装，预览保持 pending，经受控原生审阅回调后安装，首页运行和安装后验证通过。第二次真实模型调用将**同一 ID** 修改为 1.0.1，并在另一次独立审阅后安装更新。测试工具许可回调没有被调用，模型只写独立临时工作目录。

这不是桌面鼠标验收：`Manager.Decide` 由原生审阅入口下的受控测试回调触发，没有人工点击窗口，也没有 NEAR 签名或广播。因此 A20 的 Claude 模型生成、生命周期与同 ID 更新路径已取得真实模型证据，A20 的第二种真实 agent 和真实窗口操作仍未完成；A24 外部 Skill 流程也未完成。

## 尚未覆盖

- A20：Claude 的真实模型创建、预览、受控审阅、安装、运行和同 ID 更新已通过；第二种真实适配器，以及两种 agent 的真实桌面窗口操作仍未完成。
- A24：外部 agent 仅凭 Skill、连接信息、catalog 和模板完成应用。
- 其他操作系统的原生界面、进程清理与凭据服务。
- 真实系统凭据服务的部分失败和机器断电恢复；当前由自动化故障注入覆盖。

测试中的 `Manager.Decide` 仅模拟原生批准；生产 HTTP 没有批准端点。工具许可、应用安装与钱包交易始终分开处理。
