# 交卷清单 — NEAR Hackathon

这份文档给参赛者本人用，不是给评委的。功能开发在 `product-experience` 分支和 PR #21 上进行；只有在最终验证完成、明确发布后才合并 main 和打交卷标签。当前文档不把计划或单测写成真实链上验收。

截止时间：**2026-11-14 23:59 UTC**。提交前再读 <https://nearhackathon.com/HACKATHON.md>，规则和日期可能变化。

## 一、产品与赛道

项目：**Lintel（门厅）**。赛道：`security`（AI Security & Defense）。

一句话：

> Describe an app to your computer's coding agent. Lintel turns it into a native app you can preview, review and use, without giving the agent your wallet's approval power.

中文：**你告诉电脑里的 Agent 想要什么应用，它负责生成；Lintel 负责预览、权限审阅和运行，安装与钱包签名始终由人决定。**

主叙事不是普通多链钱包、支付预算、自动威胁检测或跨链成交。NEAR 的原生解码批准、function-call key 限制、隔离 WebView 与来源绑定证明人类签名边界。1Click 是 dry quote，不能称为已执行的跨链交易。

## 二、最终发布顺序

1. 在 PR #21 完成代码、真实 Agent 验收、README 复现和完整测试，再明确合并。
2. 在最终提交上创建并推送标签：

   ```sh
   git tag -a pre-hackathon ccde05d -m "Pre-hackathon baseline" # 已存在时跳过
   git tag -a hackathon-submission -m "NEAR hackathon submission"
   git push origin pre-hackathon hackathon-submission
   ```

3. 确认仓库公开，并打开 `pre-hackathon...hackathon-submission` 链接核对新增范围。
4. 准备一个有少量测试资金的 NEAR Testnet 账户，固定示例的 `RECEIVER` 改为自己控制的 testnet 账户。不要提交私钥。
5. 录 90–120 秒演示视频。真实模型生成可在视频之前完成，但画面必须诚实标为 prepared candidate；不要把准备好的候选说成现场生成。
6. 由本人在 X 发布视频，正文写明 NEAR HACKATHON，标记 `@NEARProtocol` 和 `#NEARHackathon`，附公开仓库链接。
7. 准备一个 NEAR 主网收款账户，测试网账户不能收奖金。
8. 提交公开仓库与公开 X 演示帖链接。填错走 <https://nearhackathon.com/request-changes>，不要重复提交同一仓库。

## 三、90–120 秒演示脚本

| 时间 | 画面 | 旁白 |
|---|---|---|
| 0–12s | Apps → Create app，显示需求与本地 ACP Agent 配置 | 你告诉电脑里的 Agent 想要什么应用，它会在独立工作目录生成应用。Lintel 把结果变成原生应用，而不是一段任意网页。 |
| 12–28s | 已准备好的 pending candidate → Preview，展示真实原生页面 | 生成后先冻结候选并预览实际页面。预览禁止签名，不安装，也不等于批准。这个候选是录制前准备好的。 |
| 28–40s | Review permissions，原生权限窗显示权限，点击批准 | 编码工具许可不等于安装许可。应用只有在独立的原生权限审阅后才进入 Apps。 |
| 40–52s | 打开生成的应用，展示 sparkline / gauge / 页面和运行验证 | 应用现在可以长期打开和继续修改，页面由壳绘制。运行检查通过只说明页面能执行，不代表模型或数据被审计。 |
| 52–70s | 固定 NEAR Approval Demo → Request 0.001 NEAR → 原生批准窗 → Reject | 对需要钱包的应用，插件只能申请。壳自己解码网络、接收方、金额和来源；拒绝就没有交易 hash。 |
| 70–88s | 同一合法小额请求 → Approve → hash → nearblocks testnet | 明确批准后才会签名和广播。这是真实 testnet 交易，链接可以独立核验。 |
| 88–102s | 使用受限 key 打开 Restricted key demo，展示签名前拒绝；或展示已有拒绝回归 | 如果签名 key 被限制到特定合约和方法，越权请求在批准窗前就被拒绝。它不是自动威胁评分，也不是通用支出预算。 |
| 102–110s | 回到 Apps | Agent 可以创建和请求，但不能替人安装或批准。 |

如果真实链上步骤尚未完成，不得在视频中说“已广播”或展示虚构 hash。Function-call key 需要预先准备，来不及就省略该段，不把 fake RPC 测试说成链上演示。

## 四、X 文案草稿

```text
I wanted a personal crypto tool, so I described it to my computer's coding agent.

Lintel turns the result into a native app I can preview, permission-review and use. The agent can create and request, but it cannot install apps or approve NEAR signatures for me.

Built at NEAR HACKATHON. Demo below. @NEARProtocol #NEARHackathon

Repository: https://github.com/DaviRain-Su/lintel
```

由本人检查后发布，不代发。

## 五、提交字段清单

| 字段 | 值 |
|---|---|
| 项目名 | Lintel（门厅） |
| 赛道 | `security` |
| 仓库 | <https://github.com/DaviRain-Su/lintel> |
| 演示帖 | X 发布后填写 |
| NEAR 主网收款账户 | 本人提供，不能猜造 |
| 联系邮箱 / 队长姓名 | 本人提供 |
| 团队人数 | `1` |
| 联系声明 | 本人阅读并同意后选择 |

`What did you build?` 草稿：

> Lintel turns a locally configured coding agent's output into native personal crypto apps. A user describes the tool; the agent generates an ordinary plugin in a separate workspace. Lintel validates and freezes the candidate, previews native pages with signing disabled, and installs only after separate native permission review. Installed apps run in a sandbox and can be revised without changing their identity. On NEAR, apps and web pages can request transactions but cannot access keys or approve themselves: the native shell decodes every request and signs only after human approval. Function-call key limits add protocol-enforced receiver, method and allowance boundaries. A fixed example demonstrates preview, rejection and a small testnet approval. Intents is dry-quote only. The coding agent itself is not OS-sandboxed, and preview is not a security audit.

## 六、证据清单

- [x] `pre-hackathon` 本地指向 `ccde05d`。
- [x] 单元测试覆盖冻结候选、禁签名预览、独立安装决定、拒绝不广播和 function-call key 限制。
- [x] 真实 Claude Agent 完成生成 → 预览 → 受控原生审阅 → 安装 → 打开；见 `docs/v1-acceptance.md`，没有冒充桌面鼠标验收。
- [x] 真实模型将同一应用从 1.0.0 修改为 1.0.1，并再次独立审阅。
- [ ] 真实 testnet 成功交易与 nearblocks 链接已记录。
- [ ] 真实原生窗口中用户拒绝后无交易已记录。
- [ ] NEAR Connect 在真实原生 WebView 自动发现已验证。
- [ ] 从干净克隆按 README 构建、测试并走完主要路径。
- [ ] 视频、公开仓库、两个标签、X 帖和主网收款账户均就绪。

单测中的 fake RPC 只证明程序行为，不能勾选真实链上项目。`product-experience` 已推送不等于已合并 main，不等于已发布或已提交比赛。
