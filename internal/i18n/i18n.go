// Package i18n holds the shell's own strings in English and Simplified
// Chinese. It is deliberately small and hand-written for M0; the design's
// catalog files can replace it once the plugin API needs translations too.
package i18n

import (
	"fmt"
	"strings"
)

// The languages the app ships. The empty tag follows the system.
const (
	System  = ""
	English = "en"
	Chinese = "zh-Hans"
)

// Key names one message.
type Key string

// The shell's messages.
const (
	AppTitle     Key = "appTitle"
	WatchOnly    Key = "watchOnly"
	NavPortfolio Key = "navPortfolio"
	NavWallet    Key = "navWallet"
	NavPlugins   Key = "navPlugins"
	NavSettings  Key = "navSettings"

	Refresh    Key = "refresh"
	Refreshing Key = "refreshing"

	TotalIn       Key = "totalIn"
	NoAccounts    Key = "noAccounts"
	NoAccountsWhy Key = "noAccountsWhy"
	AccountOne    Key = "accountOne"
	AccountMany   Key = "accountMany"
	AccountsTitle Key = "accountsTitle"
	AddAddress    Key = "addAddress"

	AddressPlaceholder Key = "addressPlaceholder"
	AddressLabel       Key = "addressLabel"
	LabelPlaceholder   Key = "labelPlaceholder"
	LabelLabel         Key = "labelLabel"
	Add                Key = "add"
	Remove             Key = "remove"

	UpdatedAt     Key = "updatedAt"
	NeverUpdated  Key = "neverUpdated"
	RefreshFailed Key = "refreshFailed"
	Block         Key = "block"

	InvalidAddress   Key = "invalidAddress"
	WrongFamily      Key = "wrongFamily"
	DuplicateAddress Key = "duplicateAddress"
	SaveFailed       Key = "saveFailed"

	WalletSummary        Key = "walletSummary"
	WalletSummaryOne     Key = "walletSummaryOne"
	WalletEmptySummary   Key = "walletEmptySummary"
	WalletNew            Key = "walletNew"
	WalletSecurityTitle  Key = "walletSecurityTitle"
	WalletSecurityBody   Key = "walletSecurityBody"
	WalletEmpty          Key = "walletEmpty"
	WalletEmptyWhy       Key = "walletEmptyWhy"
	WalletUnlocked       Key = "walletUnlocked"
	WalletLocked         Key = "walletLocked"
	WalletLock           Key = "walletLock"
	WalletUnlock         Key = "walletUnlock"
	WalletLockAll        Key = "walletLockAll"
	WalletReveal         Key = "walletReveal"
	WalletImport         Key = "walletImport"
	WalletImportButton   Key = "walletImportButton"
	WalletKeyLabel       Key = "walletKeyLabel"
	WalletKeyPlaceholder Key = "walletKeyPlaceholder"
	WalletImportNote     Key = "walletImportNote"
	WalletNewTitle       Key = "walletNewTitle"
	WalletRevealTitle    Key = "walletRevealTitle"
	WalletKeyWarning     Key = "walletKeyWarning"
	WalletRevealWarning  Key = "walletRevealWarning"
	WalletCopy           Key = "walletCopy"
	WalletHide           Key = "walletHide"
	WalletCopied         Key = "walletCopied"
	WalletCreated        Key = "walletCreated"
	WalletImported       Key = "walletImported"
	WalletRemoved        Key = "walletRemoved"
	WalletRemoveTitle    Key = "walletRemoveTitle"
	WalletRemoveBody     Key = "walletRemoveBody"
	WalletInvalidKey     Key = "walletInvalidKey"
	WalletDuplicate      Key = "walletDuplicate"
	WalletNoAccount      Key = "walletNoAccount"
	WalletKeychainFailed Key = "walletKeychainFailed"
	WalletUnavailable    Key = "walletUnavailable"
	WalletBadge          Key = "walletBadge"
	WalletEVMOnly        Key = "walletEVMOnly"

	AuthTouchID      Key = "authTouchID"
	AuthFaceID       Key = "authFaceID"
	AuthOpticID      Key = "authOpticID"
	AuthSystem       Key = "authSystem"
	AuthTitle        Key = "authTitle"
	AuthBody         Key = "authBody"
	AuthUnavailable  Key = "authUnavailable"
	AuthReasonUnlock Key = "authReasonUnlock"
	AuthReasonReveal Key = "authReasonReveal"
	AuthReasonSign   Key = "authReasonSign"
	AuthCanceled     Key = "authCanceled"
	AuthFailed       Key = "authFailed"
	AuthWaiting      Key = "authWaiting"
	AuthApprove      Key = "authApprove"
	AuthProtected    Key = "authProtected"
	SecuritySection  Key = "securitySection"

	ApproveWindowTitle    Key = "approveWindowTitle"
	ApproveTransferTitle  Key = "approveTransferTitle"
	ApproveAllowanceTitle Key = "approveAllowanceTitle"
	ApproveSendTitle      Key = "approveSendTitle"
	ApproveTxTitle        Key = "approveTxTitle"
	ApproveFrom           Key = "approveFrom"
	ApproveTo             Key = "approveTo"
	ApproveCall           Key = "approveCall"
	ApproveValue          Key = "approveValue"
	ApproveNetwork        Key = "approveNetwork"
	ApproveFee            Key = "approveFee"
	ApprovePluginSays     Key = "approvePluginSays"
	ApprovePluginWords    Key = "approvePluginWords"
	ApproveWarning        Key = "approveWarning"
	ApproveReject         Key = "approveReject"
	ApproveSign           Key = "approveSign"

	FamilyLabel    Key = "familyLabel"
	ChainLabel     Key = "chainLabel"
	RPCLabel       Key = "rpcLabel"
	RPCPlaceholder Key = "rpcPlaceholder"
	RPCHelp        Key = "rpcHelp"
	Apply          Key = "apply"
	Reset          Key = "reset"
	InvalidURL     Key = "invalidURL"
	RPCSaved       Key = "rpcSaved"

	RefreshInterval Key = "refreshInterval"
	Language        Key = "language"
	SystemLanguage  Key = "systemLanguage"
	NetworkSection  Key = "networkSection"
	AppSection      Key = "appSection"
	About           Key = "about"
	AboutBody       Key = "aboutBody"
	Version         Key = "version"

	Seconds Key = "seconds"
	Minutes Key = "minutes"

	PluginCount         Key = "pluginCount"
	PluginInstall       Key = "pluginInstall"
	PluginInstallTitle  Key = "pluginInstallTitle"
	PluginInstalled     Key = "pluginInstalled"
	PluginInstallFailed Key = "pluginInstallFailed"
	PluginEmpty         Key = "pluginEmpty"
	PluginEmptyWhy      Key = "pluginEmptyWhy"
	PluginOpenStore     Key = "pluginOpenStore"
	PluginUnsigned      Key = "pluginUnsigned"
	PluginLogs          Key = "pluginLogs"
	PluginLogsEmpty     Key = "pluginLogsEmpty"
	PluginRemoveTitle   Key = "pluginRemoveTitle"
	PluginRemoveBody    Key = "pluginRemoveBody"
	PluginRemoved       Key = "pluginRemoved"
	PluginRestarted     Key = "pluginRestarted"
	PluginSettingsSaved Key = "pluginSettingsSaved"
	PluginSave          Key = "pluginSave"
	PluginBack          Key = "pluginBack"
	PluginDisabled      Key = "pluginDisabled"
	Cancel              Key = "cancel"

	InstalledTab       Key = "installedTab"
	StoreTab           Key = "storeTab"
	StoreRefresh       Key = "storeRefresh"
	StoreLoading       Key = "storeLoading"
	StoreEmpty         Key = "storeEmpty"
	StoreEmptyWhy      Key = "storeEmptyWhy"
	StoreLoadFailed    Key = "storeLoadFailed"
	StoreRetry         Key = "storeRetry"
	StoreNoRegistry    Key = "storeNoRegistry"
	StoreNoRegistryWhy Key = "storeNoRegistryWhy"
	StoreOpenSettings  Key = "storeOpenSettings"
	StoreSource        Key = "storeSource"
	StoreSourceFolder  Key = "storeSourceFolder"
	StoreBy            Key = "storeBy"
	StoreInstall       Key = "storeInstall"
	StoreUpdate        Key = "storeUpdate"
	StoreInstalled     Key = "storeInstalled"
	StoreInstalling    Key = "storeInstalling"
	StoreUpdated       Key = "storeUpdated"
	StoreFailed        Key = "storeFailed"
	StoreCanceled      Key = "storeCanceled"

	StoreKickerInstall   Key = "storeKickerInstall"
	StoreKickerUpdate    Key = "storeKickerUpdate"
	StoreInstallTitle    Key = "storeInstallTitle"
	StoreUpdateTitle     Key = "storeUpdateTitle"
	StorePermissionTitle Key = "storePermissionTitle"
	StorePermNetwork     Key = "storePermNetwork"
	StorePermNoNetwork   Key = "storePermNoNetwork"
	StorePermChains      Key = "storePermChains"
	StorePermNoChains    Key = "storePermNoChains"
	StorePermRead        Key = "storePermRead"
	StorePermSign        Key = "storePermSign"
	StorePermStorage     Key = "storePermStorage"
	StorePermSettings    Key = "storePermSettings"
	StoreFrom            Key = "storeFrom"
	StoreNewPerms        Key = "storeNewPerms"
	StoreInstallBody     Key = "storeInstallBody"
	StoreUnsignedWarn    Key = "storeUnsignedWarn"
	StoreSignWarn        Key = "storeSignWarn"

	StoreRegistry     Key = "storeRegistry"
	StoreSettingsHelp Key = "storeSettingsHelp"
	StoreInvalidURL   Key = "storeInvalidURL"
)

var messages = map[Key]map[string]string{
	AppTitle:     {English: "Crypto Panel", Chinese: "加密面板"},
	WatchOnly:    {English: "Wallet & watch", Chinese: "钱包与只读"},
	NavPortfolio: {English: "Portfolio", Chinese: "持仓"},
	NavWallet:    {English: "Wallet", Chinese: "钱包"},
	NavPlugins:   {English: "Plugins", Chinese: "插件"},
	NavSettings:  {English: "Settings", Chinese: "设置"},

	Refresh:    {English: "Refresh", Chinese: "刷新"},
	Refreshing: {English: "Refreshing…", Chinese: "刷新中…"},

	TotalIn:       {English: "Total in %s", Chinese: "%s 合计"},
	NoAccounts:    {English: "No addresses yet", Chinese: "还没有地址"},
	NoAccountsWhy: {English: "Add a watch-only address to see its balances.", Chinese: "添加一个只读地址以查看余额。"},
	AccountOne:    {English: "1 account", Chinese: "1 个地址"},
	AccountMany:   {English: "%d accounts", Chinese: "%d 个地址"},
	AccountsTitle: {English: "Accounts", Chinese: "地址"},
	AddAddress:    {English: "Add an address", Chinese: "添加地址"},

	AddressPlaceholder: {English: "0x… or base58 address", Chinese: "0x… 或 base58 地址"},
	AddressLabel:       {English: "Address", Chinese: "地址"},
	LabelPlaceholder:   {English: "Label (optional)", Chinese: "备注（可选）"},
	LabelLabel:         {English: "Label", Chinese: "备注"},
	Add:                {English: "Add", Chinese: "添加"},
	Remove:             {English: "Remove", Chinese: "移除"},

	UpdatedAt:     {English: "Updated %s", Chinese: "更新于 %s"},
	NeverUpdated:  {English: "Not updated yet", Chinese: "尚未更新"},
	RefreshFailed: {English: "Last refresh failed", Chinese: "上次刷新失败"},
	Block:         {English: "Block %d", Chinese: "区块 %d"},

	InvalidAddress:   {English: "That is not a valid address.", Chinese: "这不是有效的地址。"},
	WrongFamily:      {English: "That address belongs to another chain type. Switch network first.", Chinese: "该地址属于另一种链类型，请先切换网络。"},
	DuplicateAddress: {English: "That address is already watched.", Chinese: "该地址已在列表中。"},
	SaveFailed:       {English: "Could not save settings.", Chinese: "无法保存设置。"},

	WalletSummary:        {English: "%d accounts · %d unlocked", Chinese: "%d 个账户 · %d 个已解锁"},
	WalletSummaryOne:     {English: "1 account · %d unlocked", Chinese: "1 个账户 · %d 个已解锁"},
	WalletEmptySummary:   {English: "No accounts yet", Chinese: "还没有账户"},
	WalletNew:            {English: "New account", Chinese: "新建账户"},
	WalletSecurityTitle:  {English: "Keys stay in the system keychain", Chinese: "密钥保存在系统钥匙串中"},
	WalletSecurityBody:   {English: "Plugins never see a key. Every signature is decoded by the shell and approved by you.", Chinese: "插件永远看不到密钥。每一笔签名都由本应用解码，并由你确认。"},
	WalletEmpty:          {English: "No accounts in the wallet", Chinese: "钱包里还没有账户"},
	WalletEmptyWhy:       {English: "Create an account or import a private key. Its key goes to the system keychain, never to settings.json.", Chinese: "新建一个账户，或导入私钥。私钥会存入系统钥匙串，绝不会写进 settings.json。"},
	WalletUnlocked:       {English: "Unlocked", Chinese: "已解锁"},
	WalletLocked:         {English: "Locked", Chinese: "已锁定"},
	WalletLock:           {English: "Lock", Chinese: "锁定"},
	WalletUnlock:         {English: "Unlock", Chinese: "解锁"},
	WalletLockAll:        {English: "Lock all", Chinese: "全部锁定"},
	WalletReveal:         {English: "Reveal key", Chinese: "查看私钥"},
	WalletImport:         {English: "Import a private key", Chinese: "导入私钥"},
	WalletImportButton:   {English: "Import", Chinese: "导入"},
	WalletKeyLabel:       {English: "Private key", Chinese: "私钥"},
	WalletKeyPlaceholder: {English: "64 hex characters", Chinese: "64 位十六进制字符"},
	WalletImportNote:     {English: "The key is written to the system keychain, never to settings.json.", Chinese: "私钥会写入系统钥匙串，绝不会写进 settings.json。"},
	WalletNewTitle:       {English: "Save this private key", Chinese: "请保存这个私钥"},
	WalletRevealTitle:    {English: "Private key for %s", Chinese: "%s 的私钥"},
	WalletKeyWarning:     {English: "Save it now. Anyone who has it controls the account.", Chinese: "请立即保存。任何拿到它的人都能控制这个账户。"},
	WalletRevealWarning:  {English: "Anyone who sees this key controls the account.", Chinese: "任何看到这个私钥的人都能控制这个账户。"},
	WalletCopy:           {English: "Copy", Chinese: "复制"},
	WalletHide:           {English: "Done", Chinese: "完成"},
	WalletCopied:         {English: "Copied to the clipboard.", Chinese: "已复制到剪贴板。"},
	WalletCreated:        {English: "Created %s.", Chinese: "已创建 %s。"},
	WalletImported:       {English: "Imported %s.", Chinese: "已导入 %s。"},
	WalletRemoved:        {English: "Removed %s.", Chinese: "已移除 %s。"},
	WalletRemoveTitle:    {English: "Remove wallet account?", Chinese: "移除钱包账户？"},
	WalletRemoveBody:     {English: "Remove %s? Its key is deleted from the keychain and the address stops being watched.", Chinese: "移除 %s？它的私钥会从钥匙串中删除，地址也不再被监视。"},
	WalletInvalidKey:     {English: "That is not a valid private key.", Chinese: "这不是有效的私钥。"},
	WalletDuplicate:      {English: "That account is already in the wallet.", Chinese: "该账户已在钱包中。"},
	WalletNoAccount:      {English: "That account is not in the wallet.", Chinese: "该账户不在钱包中。"},
	WalletKeychainFailed: {English: "The system keychain refused: %s", Chinese: "系统钥匙串出错：%s"},
	WalletUnavailable:    {English: "The wallet is unavailable.", Chinese: "钱包不可用。"},
	WalletBadge:          {English: "Wallet", Chinese: "钱包"},
	WalletEVMOnly:        {English: "Wallet accounts are EVM accounts. Switch to an EVM network to read their balances.", Chinese: "钱包账户是 EVM 账户，切换到 EVM 网络才能读取余额。"},

	AuthTouchID:      {English: "Touch ID", Chinese: "触控 ID"},
	AuthFaceID:       {English: "Face ID", Chinese: "面容 ID"},
	AuthOpticID:      {English: "Optic ID", Chinese: "光学 ID"},
	AuthSystem:       {English: "System authentication", Chinese: "系统认证"},
	AuthTitle:        {English: "Require %s", Chinese: "需要%s"},
	AuthBody:         {English: "Unlocking, revealing a key and every signature ask for %s.", Chinese: "解锁、查看私钥和每一次签名都需要%s。"},
	AuthUnavailable:  {English: "This device offers no system authentication, so the wallet cannot require it.", Chinese: "此设备不支持系统认证，钱包无法启用。"},
	AuthReasonUnlock: {English: "Unlock %s", Chinese: "解锁 %s"},
	AuthReasonReveal: {English: "Reveal the private key of %s", Chinese: "查看 %s 的私钥"},
	AuthReasonSign:   {English: "Approve the transaction from %s", Chinese: "确认来自 %s 的交易"},
	AuthCanceled:     {English: "Authentication canceled.", Chinese: "已取消验证。"},
	AuthFailed:       {English: "Authentication failed: %s", Chinese: "验证失败：%s"},
	AuthWaiting:      {English: "Waiting for %s…", Chinese: "等待%s…"},
	AuthApprove:      {English: "Approve with %s", Chinese: "用%s确认"},
	AuthProtected:    {English: "Protected by %s", Chinese: "受%s保护"},
	SecuritySection:  {English: "Security", Chinese: "安全"},

	ApproveWindowTitle:    {English: "Approve transaction", Chinese: "确认交易"},
	ApproveTransferTitle:  {English: "Transfer tokens?", Chinese: "转账代币？"},
	ApproveAllowanceTitle: {English: "Approve a token allowance?", Chinese: "授权代币额度？"},
	ApproveSendTitle:      {English: "Send %s?", Chinese: "发送 %s？"},
	ApproveTxTitle:        {English: "Sign this transaction?", Chinese: "签署这笔交易？"},
	ApproveFrom:           {English: "From", Chinese: "从"},
	ApproveTo:             {English: "To", Chinese: "到"},
	ApproveCall:           {English: "Call", Chinese: "调用"},
	ApproveValue:          {English: "Value", Chinese: "金额"},
	ApproveNetwork:        {English: "Network", Chinese: "网络"},
	ApproveFee:            {English: "Max fee", Chinese: "最高手续费"},
	ApprovePluginSays:     {English: "The plugin says", Chinese: "插件说"},
	ApprovePluginWords:    {English: "The plugin's words are not verified. The shell's decode above is what gets signed.", Chinese: "插件的话未经核实。上方由本应用解码的内容才是将要签名的内容。"},
	ApproveWarning:        {English: "Only approve if you trust this plugin and recognize this transaction.", Chinese: "只有在你信任该插件并认得这笔交易时才确认。"},
	ApproveReject:         {English: "Reject", Chinese: "拒绝"},
	ApproveSign:           {English: "Approve and sign", Chinese: "确认并签名"},

	FamilyLabel:    {English: "Chain type", Chinese: "链类型"},
	ChainLabel:     {English: "Chain", Chinese: "网络"},
	RPCLabel:       {English: "RPC endpoint", Chinese: "RPC 节点"},
	RPCPlaceholder: {English: "Default endpoint", Chinese: "默认节点"},
	RPCHelp:        {English: "The endpoint must serve the selected chain; a mismatch is refused.", Chinese: "节点必须与所选网络一致，不一致会被拒绝。"},
	Apply:          {English: "Apply", Chinese: "应用"},
	Reset:          {English: "Use default", Chinese: "恢复默认"},
	InvalidURL:     {English: "Enter an http(s) URL.", Chinese: "请输入 http(s) 地址。"},
	RPCSaved:       {English: "Endpoint saved.", Chinese: "节点已保存。"},

	RefreshInterval: {English: "Refresh every", Chinese: "刷新间隔"},
	Language:        {English: "Language", Chinese: "语言"},
	SystemLanguage:  {English: "System", Chinese: "跟随系统"},
	NetworkSection:  {English: "Network", Chinese: "网络"},
	AppSection:      {English: "App", Chinese: "应用"},
	About:           {English: "About", Chinese: "关于"},
	AboutBody:       {English: "Self-custody. Keys stay in the system keychain and never reach a plugin.", Chinese: "自托管。密钥保存在系统钥匙串中，插件永远无法触及。"},
	Version:         {English: "Version %s", Chinese: "版本 %s"},

	Seconds: {English: "%ds", Chinese: "%d 秒"},
	Minutes: {English: "%dm", Chinese: "%d 分钟"},

	PluginCount:         {English: "%d installed · %d on", Chinese: "%d 个已安装 · %d 个已启用"},
	PluginInstall:       {English: "Install from folder…", Chinese: "从文件夹安装…"},
	PluginInstallTitle:  {English: "Choose a plugin folder", Chinese: "选择插件文件夹"},
	PluginInstalled:     {English: "Installed %s.", Chinese: "已安装 %s。"},
	PluginInstallFailed: {English: "Install failed: %s", Chinese: "安装失败：%s"},
	PluginEmpty:         {English: "No plugins installed", Chinese: "还没有安装插件"},
	PluginEmptyWhy:      {English: "A plugin is a folder with plugin.json and a JavaScript file. Install one from a folder, or browse the store.", Chinese: "插件是包含 plugin.json 和 JavaScript 文件的文件夹。可以从文件夹安装，也可以去商店看看。"},
	PluginOpenStore:     {English: "Open store", Chinese: "打开商店"},
	PluginUnsigned:      {English: "Unsigned", Chinese: "未签名"},
	PluginLogs:          {English: "Logs", Chinese: "日志"},
	PluginLogsEmpty:     {English: "Nothing logged yet.", Chinese: "还没有日志。"},
	PluginRemoveTitle:   {English: "Remove plugin?", Chinese: "移除插件？"},
	PluginRemoveBody:    {English: "Remove %s? Its folder and saved settings are deleted.", Chinese: "移除 %s？它的文件夹和已保存的设置都会被删除。"},
	PluginRemoved:       {English: "Removed %s.", Chinese: "已移除 %s。"},
	PluginRestarted:     {English: "Restarted %d×", Chinese: "已重启 %d 次"},
	PluginSettingsSaved: {English: "Settings saved.", Chinese: "设置已保存。"},
	PluginSave:          {English: "Save", Chinese: "保存"},
	PluginBack:          {English: "Back", Chinese: "返回"},
	PluginDisabled:      {English: "This plugin is turned off.", Chinese: "该插件已停用。"},
	Cancel:              {English: "Cancel", Chinese: "取消"},

	InstalledTab:       {English: "Installed", Chinese: "已安装"},
	StoreTab:           {English: "Store", Chinese: "商店"},
	StoreRefresh:       {English: "Refresh store", Chinese: "刷新商店"},
	StoreLoading:       {English: "Loading the store…", Chinese: "正在加载商店…"},
	StoreEmpty:         {English: "The store is empty", Chinese: "商店里还没有插件"},
	StoreEmptyWhy:      {English: "The registry lists no plugins yet.", Chinese: "插件源里还没有列出任何插件。"},
	StoreLoadFailed:    {English: "Could not load the store: %s", Chinese: "无法加载商店：%s"},
	StoreRetry:         {English: "Retry", Chinese: "重试"},
	StoreNoRegistry:    {English: "No plugin registry yet", Chinese: "还没有设置插件源"},
	StoreNoRegistryWhy: {English: "The app ships with no plugins. Add a registry URL in Settings to browse signed plugins, or install one from a folder.", Chinese: "应用默认不带任何插件。在设置里填入插件源地址即可浏览已签名插件，也可以从文件夹安装。"},
	StoreOpenSettings:  {English: "Open Settings", Chinese: "打开设置"},
	StoreSource:        {English: "Registry: %s", Chinese: "插件源：%s"},
	StoreSourceFolder:  {English: "a folder", Chinese: "本地文件夹"},
	StoreBy:            {English: "by %s", Chinese: "作者 %s"},
	StoreInstall:       {English: "Install", Chinese: "安装"},
	StoreUpdate:        {English: "Update", Chinese: "更新"},
	StoreInstalled:     {English: "Installed", Chinese: "已安装"},
	StoreInstalling:    {English: "Installing %s…", Chinese: "正在安装 %s…"},
	StoreUpdated:       {English: "Updated %s to v%s.", Chinese: "已将 %s 更新到 v%s。"},
	StoreFailed:        {English: "Install failed: %s", Chinese: "安装失败：%s"},
	StoreCanceled:      {English: "Install canceled.", Chinese: "已取消安装。"},

	StoreKickerInstall:   {English: "Plugin store", Chinese: "插件商店"},
	StoreKickerUpdate:    {English: "Plugin update", Chinese: "插件更新"},
	StoreInstallTitle:    {English: "Install %s?", Chinese: "安装 %s？"},
	StoreUpdateTitle:     {English: "Update %s to v%s?", Chinese: "将 %s 更新到 v%s？"},
	StorePermissionTitle: {English: "This plugin can", Chinese: "该插件可以"},
	StorePermNetwork:     {English: "Network: %s", Chinese: "网络：%s"},
	StorePermNoNetwork:   {English: "No network access", Chinese: "无网络访问"},
	StorePermChains:      {English: "Chains: %s", Chinese: "链：%s"},
	StorePermNoChains:    {English: "No chain access", Chinese: "无链上访问"},
	StorePermRead:        {English: "See your watched addresses and balances", Chinese: "查看你监视的地址和余额"},
	StorePermSign:        {English: "Ask you to sign transactions", Chinese: "请求你签名交易"},
	StorePermStorage:     {English: "Store %d KB of data", Chinese: "存储 %d KB 数据"},
	StorePermSettings:    {English: "Settings: %d", Chinese: "设置：%d"},
	StoreFrom:            {English: "From: %s", Chinese: "来源：%s"},
	StoreNewPerms:        {English: "New permissions: %s", Chinese: "新增权限：%s"},
	StoreInstallBody:     {English: "Only what is listed here is granted. A plugin never sees your keys.", Chinese: "只授予上面列出的权限。插件永远看不到你的私钥。"},
	StoreUnsignedWarn:    {English: "This plugin is not signed. Install it only if you trust where it came from.", Chinese: "该插件未签名，只有在你信任来源时才安装。"},
	StoreSignWarn:        {English: "Every signature still needs your approval in a window the shell draws.", Chinese: "每一笔签名仍需你在本应用绘制的窗口中确认。"},

	StoreRegistry:     {English: "Plugin registry", Chinese: "插件源"},
	StoreSettingsHelp: {English: "A registry is a JSON file that lists signed plugins. Leave empty for no store.", Chinese: "插件源是一个列出已签名插件的 JSON 文件。留空则不启用商店。"},
	StoreInvalidURL:   {English: "Enter an https registry URL.", Chinese: "请输入 https 插件源地址。"},
}

// T says key in lang, in English if lang has no translation, with args
// applied by fmt.Sprintf when there are any.
func T(lang string, key Key, args ...any) string {
	byLang, ok := messages[key]
	if !ok {
		return string(key)
	}
	s := byLang[lang]
	if s == "" {
		s = byLang[English]
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// Resolve picks the language to show: the user's choice when it names one
// of the app's, else Chinese for a Chinese system language, else English.
func Resolve(setting, system string) string {
	switch setting {
	case English, Chinese:
		return setting
	}
	tag := strings.ToLower(system)
	if strings.HasPrefix(tag, "zh") {
		return Chinese
	}
	return English
}

// Languages lists the choices of the settings, in the order they show.
func Languages() []string { return []string{System, English, Chinese} }
