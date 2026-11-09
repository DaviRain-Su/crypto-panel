# 交卷清单 — NEAR Hackathon

这份文档给参赛者本人用，不是给评委的。代码已经完成并通过测试。提交和推送已按你的
要求完成；剩下的发布动作由你来做：打标签、公开仓库、发 X、准备主网收款账户、填表。

截止时间：**2026-11-14 23:59 UTC**。提交前再读一遍
<https://nearhackathon.com/HACKATHON.md>，规则和日期可能改。

## 一、交卷前你要做的动作

按顺序做，前一步没完成不要做下一步。

1. **确认提交已在。** 工作区已提交并推送到远程 `main`。如果之后还有改动：

   ```sh
   git add -A
   git commit -m "feat: NEAR wallet, native approvals, DApp bridge and Intents dry quote"
   git push origin HEAD:main
   ```

2. **打交卷标签。**

   ```sh
   git tag -a hackathon-submission -m "NEAR hackathon submission"
   ```

3. **推送标签。** README 里的 diff 链接
   `compare/pre-hackathon...hackathon-submission` 只有在两个标签都推到公开仓库后
   才有效；`pre-hackathon` 已在本地，指向 `ccde05d`。

   ```sh
   git push origin pre-hackathon hackathon-submission
   ```

4. **确认仓库是公开的。** README 里的链接、diff 和评委复现都依赖这一点。

5. **录一段不超过 90 秒的演示视频**，脚本见第三节。

6. **发 X 演示帖**，要点见第四节。助手不代发。

7. **准备一个 NEAR 主网收款账户**（具名 `xxx.near` 或 64 位隐式账户）。测试网
   账户不能收奖金。

8. **填写并提交参赛表。** 字段见第二节。填错走
   <https://nearhackathon.com/request-changes>，不要用同一个仓库再交一次。

## 二、提交表字段清单

以表单实际字段为准；提交前把完整 payload 给自己过一遍。

| 字段 | 值 |
|---|---|
| 项目名 | Crypto Panel |
| 赛道 | `security`（主）、`defi`（次） |
| 仓库地址 | <https://github.com/DaviRain-Su/crypto-panel> |
| 演示帖链接 | 占位 — X 帖发布后填入 |
| NEAR 主网收款账户 | **由你填写**（具名或 64 位隐式） |
| 联系邮箱 | **由你填写** |
| 队长姓名 | **由你填写** |
| 团队人数 | `1` |
| 同意主办方联系声明 | **由你阅读并勾选** |

提交接口是 `https://nearhackathon.com/api/applications`。助手不会调用它，也不会
代你点提交。

## 三、90 秒演示脚本

旁白按中文写，录屏时可以说英文或中文；每一步都对应 README 的复现步骤。

| 时间 | 画面 | 旁白 |
|---|---|---|
| 0–10s | 打开 crypto-panel，侧边栏扫过 Portfolio / Wallet / Apps / Intents / DApp | 用链上产品时，你不必先搞懂钱包和哪条链。这个桌面壳把钥匙和批准留在本机，链在背后。 |
| 10–25s | Wallet → New wallet，切到 NEAR，显示 64 位隐式账户和余额 | 新建钱包会同时准备 EVM、NEAR 和 Solana 的钥匙。切到 NEAR 看到隐式账户。钥匙在系统钥匙串里，插件和网页都看不到。 |
| 25–45s | Send NEAR → 输入 `0.1` → 批准窗（网络、账户、接收方、NEAR 和 yoctoNEAR、来源）→ Approve → 点 View transaction 打开 nearblocks | 转一笔测试网 NEAR。壳自己解码并画出批准窗；批准后签名广播，拒绝就什么都不发。 |
| 45–60s | DApp → 打开 `http://localhost:8080` → Connect wallet → Send 0.01 NEAR → 原生批准窗 | 网页只能申请。它拿不到钥匙，也不能自己点通过；批准窗是壳画的。 |
| 60–75s | 用受限的 function-call key 触发一次超额或未授权方法，界面显示拒绝 | 如果钥匙被限制在某个合约、某些方法和额度内，超出的请求在弹窗前就被拒绝。 |
| 75–90s | Intents → 选 wNEAR → USDC，Get a dry quote → 批准窗「You pay 1 wNEAR / You receive at least … USDC」 | 跨链兑换用 NEAR Intents：批准窗直接用「付出多少、至少换回多少」说话。这次只跑 dry quote，不签、不动钱。 |

收尾一句：插件和网页只能申请，真正的钥匙和批准在壳里。

## 四、X 帖要点

- 正文第一句讲用户问题：「用链上产品时，不必先搞懂钱包和哪条链」。
- 带上 90 秒视频。
- 写明做于 NEAR HACKATHON，标记 `@NEARProtocol` 和 `#NEARHackathon`。
- 附公开仓库链接。
- 由你发布，助手不代发。

## 五、验收清单（当前状态）

- [x] `pre-hackathon` 指向加入 NEAR 之前的提交（`ccde05d`）
- [x] 测试网：创建或导入账户，能显示余额（`TestNearWalletCreate`、`TestNearWalletImport`）
- [x] 测试网转账：批准后链上可查；拒绝后无交易（`TestSendNearSignsAfterApproval`、`TestSendNearRejected`）
- [x] 批准窗展示网络、账户、接收方、金额、来源；未知动作不假装已解码（`TestApprovalViewShowsTheNearDecode`）
- [x] function-call key：额度内成功，超额或方法不匹配被拒绝并可感知（`TestPrepareAppliesFunctionCallKeyLimits`、`TestSignNearTransactionAppliesTheKeyLimit`）
- [x] 私钥不出现在插件、WebView、日志、仓库里（`TestNearWalletCreate` 断言 `settings.json` 不含 `ed25519:`；`.gitignore` 排除 `.env` 和 `*.key`）
- [x] 隔离 WebView 能连接并完成一次测试网签名，页面不能自批（`TestDappSignNeedsAConnection`、`TestDappSignRejected`、`TestDappBridgeScript`）
- [x] Intents：`dry: true` 询价在批准窗用结果语言展示（`TestQuoteAsksForADryQuote`、`TestIntentsQuoteShowsResultLanguage`）；没有主网成交
- [x] Passkey：真正的 WebAuthn Passkey 钱包（`TestCreatePasskeyStoresAndWatchesTheAccounts`、`TestSendPasskeyNearSignsAfterApproval`）。同一把 P-256 钥匙控制 NEAR 主网 `0s…` 账户（NEP-616 + NEP-591）和所有 EVM 链上同地址的 ERC-4337 智能账户；NEAR 部署/转账由钱包自己的中继账户代付 gas，拒绝后不广播。EVM 发送需要 bundler，未接线；Touch ID 仍是系统存在性检查，不是这把 Passkey；README 已写明恢复规则
- [x] README 可按步骤复现；有 LICENSE；写明基线、限制，以及「无感不等于跳过批准」
- [x] 有针对拒绝、限额、钥匙不落盘的测试
- [x] 没有接入 Phoenix / Hyperliquid / Solana 签名，也没有接 Chain Signatures
- [ ] 交卷动作留给用户：公开仓库、X 帖、主网收款账户、确认后的表单

## 六、助手不代做的事

提交和推送已按你的明确要求完成。以下仍由你来做：

- 创建主网收款账户。
- 发 X 帖。
- 把仓库改成公开。
- 打 `hackathon-submission` 标签并推送。
- 调用提交接口，代填表单。
- 提交任何私钥、助记词或付费 RPC token。
