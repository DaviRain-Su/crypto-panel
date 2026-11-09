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

## 尚未覆盖

- A20：两种真实适配器分别完成需求 → 文件 → 校验 → 原生确认 → 安装 → 查看 → 同 id 更新，记录版本与平台。
- A24：外部 agent 仅凭 Skill、连接信息、catalog 和模板完成应用。
- 其他操作系统的原生界面、进程清理与凭据服务。
- 真实系统凭据服务的部分失败和机器断电恢复；当前由自动化故障注入覆盖。

测试中的 `Manager.Decide` 仅模拟原生批准；生产 HTTP 没有批准端点。工具许可、应用安装与钱包交易始终分开处理。
