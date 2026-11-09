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
	AppTitle        Key = "appTitle"
	WatchOnly       Key = "watchOnly"
	NavPortfolio    Key = "navPortfolio"
	NavWallet       Key = "navWallet"
	NavApps         Key = "navApps"
	NavDapp         Key = "navDapp"
	NavSettings     Key = "navSettings"
	SidebarCollapse Key = "sidebarCollapse"
	SidebarExpand   Key = "sidebarExpand"

	AppsSubtitle       Key = "appsSubtitle"
	DappURLLabel       Key = "dappURLLabel"
	DappURLPlaceholder Key = "dappURLPlaceholder"
	DappOpen           Key = "dappOpen"
	DappClose          Key = "dappClose"
	DappEmpty          Key = "dappEmpty"
	DappEmptyWhy       Key = "dappEmptyWhy"
	DappInvalidURL     Key = "dappInvalidURL"
	DappConnectTitle   Key = "dappConnectTitle"
	DappConnectBody    Key = "dappConnectBody"
	DappConnect        Key = "dappConnect"
	DappNearOnly       Key = "dappNearOnly"
	DappSwitchNear     Key = "dappSwitchNear"

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

	WalletSummary                Key = "walletSummary"
	WalletSummaryOne             Key = "walletSummaryOne"
	WalletEmptySummary           Key = "walletEmptySummary"
	WalletNew                    Key = "walletNew"
	WalletSecurityTitle          Key = "walletSecurityTitle"
	WalletSecurityBody           Key = "walletSecurityBody"
	WalletEmpty                  Key = "walletEmpty"
	WalletEmptyWhy               Key = "walletEmptyWhy"
	WalletUnlocked               Key = "walletUnlocked"
	WalletLocked                 Key = "walletLocked"
	WalletLock                   Key = "walletLock"
	WalletUnlock                 Key = "walletUnlock"
	WalletLockAll                Key = "walletLockAll"
	WalletReveal                 Key = "walletReveal"
	WalletImport                 Key = "walletImport"
	WalletImportButton           Key = "walletImportButton"
	WalletKeyLabel               Key = "walletKeyLabel"
	WalletKeyPlaceholder         Key = "walletKeyPlaceholder"
	WalletImportNote             Key = "walletImportNote"
	WalletNewTitle               Key = "walletNewTitle"
	WalletNewTitleMany           Key = "walletNewTitleMany"
	WalletSwitch                 Key = "walletSwitch"
	WalletImportInto             Key = "walletImportInto"
	WalletImportChain            Key = "walletImportChain"
	WalletNoChain                Key = "walletNoChain"
	WalletNoChainWhy             Key = "walletNoChainWhy"
	WalletSolanaKeyLabel         Key = "walletSolanaKeyLabel"
	WalletSolanaKeyPlaceholder   Key = "walletSolanaKeyPlaceholder"
	WalletSolanaImportNote       Key = "walletSolanaImportNote"
	WalletSolanaNote             Key = "walletSolanaNote"
	WalletRevealTitle            Key = "walletRevealTitle"
	WalletKeyWarning             Key = "walletKeyWarning"
	WalletRevealWarning          Key = "walletRevealWarning"
	WalletCopy                   Key = "walletCopy"
	WalletHide                   Key = "walletHide"
	WalletCopied                 Key = "walletCopied"
	WalletCreated                Key = "walletCreated"
	WalletImported               Key = "walletImported"
	WalletRemoved                Key = "walletRemoved"
	WalletRemoveTitle            Key = "walletRemoveTitle"
	WalletRemoveBody             Key = "walletRemoveBody"
	WalletInvalidKey             Key = "walletInvalidKey"
	WalletDuplicate              Key = "walletDuplicate"
	WalletNoAccount              Key = "walletNoAccount"
	WalletKeychainFailed         Key = "walletKeychainFailed"
	WalletUnavailable            Key = "walletUnavailable"
	WalletBadge                  Key = "walletBadge"
	WalletEVMOnly                Key = "walletEVMOnly"
	WalletEVMSection             Key = "walletEVMSection"
	WalletNearSection            Key = "walletNearSection"
	WalletNearNew                Key = "walletNearNew"
	WalletNearBadge              Key = "walletNearBadge"
	WalletNearImport             Key = "walletNearImport"
	WalletNearKeyLabel           Key = "walletNearKeyLabel"
	WalletNearKeyPlaceholder     Key = "walletNearKeyPlaceholder"
	WalletNearAccountLabel       Key = "walletNearAccountLabel"
	WalletNearAccountPlaceholder Key = "walletNearAccountPlaceholder"
	WalletNearImportNote         Key = "walletNearImportNote"
	WalletNearCreated            Key = "walletNearCreated"
	WalletNearImported           Key = "walletNearImported"
	WalletNearSendTitle          Key = "walletNearSendTitle"
	WalletNearSendFrom           Key = "walletNearSendFrom"
	WalletNearSendTo             Key = "walletNearSendTo"
	WalletNearSendToPlaceholder  Key = "walletNearSendToPlaceholder"
	WalletNearSendAmount         Key = "walletNearSendAmount"
	WalletNearSendAmountHolder   Key = "walletNearSendAmountHolder"
	WalletNearSend               Key = "walletNearSend"
	WalletNearSendNoNetwork      Key = "walletNearSendNoNetwork"
	WalletNearSendNoAccount      Key = "walletNearSendNoAccount"
	WalletNearSendLocked         Key = "walletNearSendLocked"
	WalletNearSendInvalidTo      Key = "walletNearSendInvalidTo"
	WalletNearSendInvalidAmount  Key = "walletNearSendInvalidAmount"
	WalletNearSendSent           Key = "walletNearSendSent"
	WalletNearSendView           Key = "walletNearSendView"
	ApproveWalletWarning         Key = "approveWalletWarning"

	PasskeyTitle          Key = "passkeyTitle"
	PasskeyBadge          Key = "passkeyBadge"
	PasskeyBody           Key = "passkeyBody"
	PasskeyCreate         Key = "passkeyCreate"
	PasskeyWaiting        Key = "passkeyWaiting"
	PasskeyFine           Key = "passkeyFine"
	PasskeyCreated        Key = "passkeyCreated"
	PasskeyFailed         Key = "passkeyFailed"
	PasskeyUnavailable    Key = "passkeyUnavailable"
	PasskeyNearAccount    Key = "passkeyNearAccount"
	PasskeyEVMAccount     Key = "passkeyEVMAccount"
	PasskeyEVMNote        Key = "passkeyEVMNote"
	PasskeySolanaAccount  Key = "passkeySolanaAccount"
	PasskeySolanaNote     Key = "passkeySolanaNote"
	PasskeyDeriving       Key = "passkeyDeriving"
	PasskeyMainnetOnly    Key = "passkeyMainnetOnly"
	PasskeyDeploy         Key = "passkeyDeploy"
	PasskeyDeployHint     Key = "passkeyDeployHint"
	PasskeyDeployNote     Key = "passkeyDeployNote"
	PasskeyDeployed       Key = "passkeyDeployed"
	PasskeyAlreadyOnChain Key = "passkeyAlreadyOnChain"
	PasskeySend           Key = "passkeySend"
	PasskeySendNote       Key = "passkeySendNote"
	PasskeyNoRelayer      Key = "passkeyNoRelayer"
	PasskeyRelayerLocked  Key = "passkeyRelayerLocked"
	PasskeyRejected       Key = "passkeyRejected"
	PasskeyForget         Key = "passkeyForget"
	PasskeyForgetTitle    Key = "passkeyForgetTitle"
	PasskeyForgetBody     Key = "passkeyForgetBody"
	PasskeySettingsTitle  Key = "passkeySettingsTitle"
	PasskeySettingsBody   Key = "passkeySettingsBody"
	PasskeySettingsHave   Key = "passkeySettingsHave"

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
	ApproveDappKicker     Key = "approveDappKicker"
	ApproveSiteSays       Key = "approveSiteSays"
	ApproveSiteWords      Key = "approveSiteWords"
	ApproveDappWarning    Key = "approveDappWarning"

	ApproveReceiver           Key = "approveReceiver"
	ApproveSigningKey         Key = "approveSigningKey"
	ApproveKeyLimit           Key = "approveKeyLimit"
	ApproveAmount             Key = "approveAmount"
	ApproveYocto              Key = "approveYocto"
	ApproveAction             Key = "approveAction"
	ApproveActionCall         Key = "approveActionCall"
	ApproveActionAddKey       Key = "approveActionAddKey"
	ApproveActionDeleteKey    Key = "approveActionDeleteKey"
	ApproveActionStateInit    Key = "approveActionStateInit"
	ApproveStateInitSize      Key = "approveStateInitSize"
	ApproveMethod             Key = "approveMethod"
	ApproveArgs               Key = "approveArgs"
	ApproveDeposit            Key = "approveDeposit"
	ApproveGas                Key = "approveGas"
	ApproveKey                Key = "approveKey"
	ApproveKeyContract        Key = "approveKeyContract"
	ApproveKeyMethods         Key = "approveKeyMethods"
	ApproveKeyAllowance       Key = "approveKeyAllowance"
	ApproveKeyPermission      Key = "approveKeyPermission"
	ApproveFullAccess         Key = "approveFullAccess"
	ApproveFullAccessKey      Key = "approveFullAccessKey"
	ApproveAllowanceOf        Key = "approveAllowanceOf"
	ApproveAnyMethod          Key = "approveAnyMethod"
	ApproveNoAllowance        Key = "approveNoAllowance"
	ApproveNearCallTitle      Key = "approveNearCallTitle"
	ApproveNearKeyTitle       Key = "approveNearKeyTitle"
	ApproveNearDeleteKeyTitle Key = "approveNearDeleteKeyTitle"
	ApproveNearTxTitle        Key = "approveNearTxTitle"

	NavIntents Key = "navIntents"

	IntentsSubtitle             Key = "intentsSubtitle"
	IntentsPayWith              Key = "intentsPayWith"
	IntentsReceive              Key = "intentsReceive"
	IntentsAmount               Key = "intentsAmount"
	IntentsAmountPlaceholder    Key = "intentsAmountPlaceholder"
	IntentsRecipient            Key = "intentsRecipient"
	IntentsRecipientPlaceholder Key = "intentsRecipientPlaceholder"
	IntentsRefund               Key = "intentsRefund"
	IntentsRefundPlaceholder    Key = "intentsRefundPlaceholder"
	IntentsQuote                Key = "intentsQuote"
	IntentsQuoting              Key = "intentsQuoting"
	IntentsLoadAssets           Key = "intentsLoadAssets"
	IntentsLoadingAssets        Key = "intentsLoadingAssets"
	IntentsAssetsNote           Key = "intentsAssetsNote"
	IntentsNoAssets             Key = "intentsNoAssets"
	IntentsPickAssets           Key = "intentsPickAssets"
	IntentsAmountInvalid        Key = "intentsAmountInvalid"
	IntentsRecipientRequired    Key = "intentsRecipientRequired"
	IntentsRefundRequired       Key = "intentsRefundRequired"
	IntentsNoClient             Key = "intentsNoClient"
	IntentsRetry                Key = "intentsRetry"
	IntentsQuoteTitle           Key = "intentsQuoteTitle"
	IntentsDryBadge             Key = "intentsDryBadge"
	IntentsYouPay               Key = "intentsYouPay"
	IntentsYouReceive           Key = "intentsYouReceive"
	IntentsEstimated            Key = "intentsEstimated"
	IntentsDeadline             Key = "intentsDeadline"
	IntentsDepositAddress       Key = "intentsDepositAddress"
	IntentsNoDeposit            Key = "intentsNoDeposit"
	IntentsNoDeadline           Key = "intentsNoDeadline"
	IntentsRefundRow            Key = "intentsRefundRow"
	IntentsRecipientRow         Key = "intentsRecipientRow"
	IntentsReview               Key = "intentsReview"
	IntentsRejected             Key = "intentsRejected"
	IntentsAccepted             Key = "intentsAccepted"
	IntentsQuotedBy             Key = "intentsQuotedBy"

	IntentsApproveWindowTitle Key = "intentsApproveWindowTitle"
	IntentsApproveKicker      Key = "intentsApproveKicker"
	IntentsApproveTitle       Key = "intentsApproveTitle"
	IntentsApprovePay         Key = "intentsApprovePay"
	IntentsApproveReceive     Key = "intentsApproveReceive"
	IntentsApproveDeadline    Key = "intentsApproveDeadline"
	IntentsApproveDeposit     Key = "intentsApproveDeposit"
	IntentsApproveIntent      Key = "intentsApproveIntent"
	IntentsApproveRefund      Key = "intentsApproveRefund"
	IntentsApproveRecipient   Key = "intentsApproveRecipient"
	IntentsApproveSource      Key = "intentsApproveSource"
	IntentsApproveSourceValue Key = "intentsApproveSourceValue"
	IntentsApproveWarning     Key = "intentsApproveWarning"
	IntentsApproveAccept      Key = "intentsApproveAccept"
	IntentsIntentText         Key = "intentsIntentText"

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
	FiatTitle       Key = "fiatTitle"
	FiatBody        Key = "fiatBody"
	FiatApprox      Key = "fiatApprox"
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
	AppOpen             Key = "appOpen"
	AppSettings         Key = "appSettings"
	AppAll              Key = "appAll"
	AppOff              Key = "appOff"
	AppNoPages          Key = "appNoPages"
	AppBroken           Key = "appBroken"
	AppZip              Key = "appZip"
	AppZipTitle         Key = "appZipTitle"
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
	StoreSourceZip     Key = "storeSourceZip"
	StoreSearch        Key = "storeSearch"
	StoreNoMatches     Key = "storeNoMatches"
	StoreBy            Key = "storeBy"
	StoreInstall       Key = "storeInstall"
	StoreUpdate        Key = "storeUpdate"
	StoreInstalled     Key = "storeInstalled"
	StoreInstalling    Key = "storeInstalling"
	StoreUpdated       Key = "storeUpdated"
	StoreFailed        Key = "storeFailed"
	StoreCanceled      Key = "storeCanceled"

	AgentSection     Key = "agentSection"
	AgentTitle       Key = "agentTitle"
	AgentBody        Key = "agentBody"
	AgentPort        Key = "agentPort"
	AgentPortInvalid Key = "agentPortInvalid"
	AgentListening   Key = "agentListening"
	AgentToken       Key = "agentToken"
	AgentRotate      Key = "agentRotate"
	AgentCurl        Key = "agentCurl"

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
	AppTitle:        {English: "Crypto Panel", Chinese: "加密面板"},
	WatchOnly:       {English: "Wallet & watch", Chinese: "钱包与只读"},
	NavPortfolio:    {English: "Portfolio", Chinese: "持仓"},
	NavWallet:       {English: "Wallet", Chinese: "钱包"},
	NavApps:         {English: "Apps", Chinese: "应用"},
	NavDapp:         {English: "DApp", Chinese: "DApp"},
	NavSettings:     {English: "Settings", Chinese: "设置"},
	SidebarCollapse: {English: "Collapse sidebar", Chinese: "收起侧栏"},
	SidebarExpand:   {English: "Expand sidebar", Chinese: "展开侧栏"},

	AppsSubtitle:       {English: "Web apps, signed by the local wallet", Chinese: "由本地钱包签名的网页应用"},
	DappURLLabel:       {English: "DApp URL", Chinese: "DApp 地址"},
	DappURLPlaceholder: {English: "https://app.testnet.near.org", Chinese: "https://app.testnet.near.org"},
	DappOpen:           {English: "Open", Chinese: "打开"},
	DappClose:          {English: "Close", Chinese: "关闭"},
	DappEmpty:          {English: "Open a web app", Chinese: "打开一个网页应用"},
	DappEmptyWhy:       {English: "The page runs isolated: it cannot read your keys, files or the app's internals. Every signature it asks for is decoded and approved by the shell.", Chinese: "页面在隔离环境中运行：它读不到你的私钥、文件或应用内部。它请求的每一次签名都由本应用解码并由你确认。"},
	DappInvalidURL:     {English: "Enter an https URL.", Chinese: "请输入 https 地址。"},
	DappConnectTitle:   {English: "Connect this site?", Chinese: "连接该网站？"},
	DappConnectBody:    {English: "%s wants to see your NEAR accounts and ask you to sign. It never sees a key.", Chinese: "%s 想查看你的 NEAR 账户并请求签名。它永远看不到私钥。"},
	DappConnect:        {English: "Connect", Chinese: "连接"},
	DappNearOnly:       {English: "DApp signing works on a NEAR network. The page can still be opened, but it cannot connect.", Chinese: "DApp 签名需要 NEAR 网络。页面仍可打开，但无法连接钱包。"},
	DappSwitchNear:     {English: "Use NEAR Testnet", Chinese: "切换到 NEAR 测试网"},

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

	WalletSummary:                {English: "%d accounts · %d unlocked", Chinese: "%d 个账户 · %d 个已解锁"},
	WalletSummaryOne:             {English: "1 account · %d unlocked", Chinese: "1 个账户 · %d 个已解锁"},
	WalletEmptySummary:           {English: "No wallets yet", Chinese: "还没有钱包"},
	WalletNew:                    {English: "New wallet", Chinese: "新建钱包"},
	WalletSecurityTitle:          {English: "Keys stay in the system keychain", Chinese: "密钥保存在系统钥匙串中"},
	WalletSecurityBody:           {English: "Plugins never see a key. Every signature is decoded by the shell and approved by you.", Chinese: "插件永远看不到密钥。每一笔签名都由本应用解码，并由你确认。"},
	WalletEmpty:                  {English: "No wallets yet", Chinese: "还没有钱包"},
	WalletEmptyWhy:               {English: "Create a wallet to hold an EVM, NEAR, and Solana account together, or import a key for one chain. Keys go to the system keychain, never to settings.json.", Chinese: "新建一个钱包，同时持有 EVM、NEAR 和 Solana 账户，或为某一条链导入私钥。私钥会存入系统钥匙串，绝不会写进 settings.json。"},
	WalletUnlocked:               {English: "Unlocked", Chinese: "已解锁"},
	WalletLocked:                 {English: "Locked", Chinese: "已锁定"},
	WalletLock:                   {English: "Lock", Chinese: "锁定"},
	WalletUnlock:                 {English: "Unlock", Chinese: "解锁"},
	WalletLockAll:                {English: "Lock all", Chinese: "全部锁定"},
	WalletReveal:                 {English: "Reveal key", Chinese: "查看私钥"},
	WalletImport:                 {English: "Import a private key", Chinese: "导入私钥"},
	WalletImportButton:           {English: "Import", Chinese: "导入"},
	WalletKeyLabel:               {English: "Private key", Chinese: "私钥"},
	WalletKeyPlaceholder:         {English: "64 hex characters", Chinese: "64 位十六进制字符"},
	WalletImportNote:             {English: "The key is written to the system keychain, never to settings.json.", Chinese: "私钥会写入系统钥匙串，绝不会写进 settings.json。"},
	WalletNewTitle:               {English: "Save this private key", Chinese: "请保存这个私钥"},
	WalletNewTitleMany:           {English: "Save these private keys", Chinese: "请保存这些私钥"},
	WalletSwitch:                 {English: "Wallet", Chinese: "钱包"},
	WalletImportInto:             {English: "Import a private key into %s", Chinese: "向 %s 导入私钥"},
	WalletImportChain:            {English: "This key is for %s. Switch chain above to import a different one.", Chinese: "这把私钥属于 %s。要导入其他链，请先在上方切换。"},
	WalletNoChain:                {English: "%s has no %s account", Chinese: "%s 还没有 %s 账户"},
	WalletNoChainWhy:             {English: "Import a key for this chain below, or create a new wallet to generate one.", Chinese: "在下方为这条链导入私钥，或新建一个钱包来生成。"},
	WalletSolanaKeyLabel:         {English: "Solana secret key", Chinese: "Solana 私钥"},
	WalletSolanaKeyPlaceholder:   {English: "base58 64-byte keypair", Chinese: "base58 编码的 64 字节密钥对"},
	WalletSolanaImportNote:       {English: "Phantom and solana-keygen export a base58 64-byte keypair. A JSON array of those 64 bytes, or a 32-byte seed, also works. The key is written to the system keychain, never to settings.json.", Chinese: "Phantom 和 solana-keygen 导出的是 base58 编码的 64 字节密钥对。64 字节的 JSON 数组或 32 字节种子也可以。私钥会写入系统钥匙串，绝不会写进 settings.json。"},
	WalletSolanaNote:             {English: "The key is in the system keychain. Sending a Solana transaction is not wired in this version.", Chinese: "私钥在系统钥匙串里。此版本还不能发送 Solana 交易。"},
	WalletRevealTitle:            {English: "Private key for %s", Chinese: "%s 的私钥"},
	WalletKeyWarning:             {English: "Save it now. Anyone who has it controls the account.", Chinese: "请立即保存。任何拿到它的人都能控制这个账户。"},
	WalletRevealWarning:          {English: "Anyone who sees this key controls the account.", Chinese: "任何看到这个私钥的人都能控制这个账户。"},
	WalletCopy:                   {English: "Copy", Chinese: "复制"},
	WalletHide:                   {English: "Done", Chinese: "完成"},
	WalletCopied:                 {English: "Copied to the clipboard.", Chinese: "已复制到剪贴板。"},
	WalletCreated:                {English: "Created %s.", Chinese: "已创建 %s。"},
	WalletImported:               {English: "Imported %s.", Chinese: "已导入 %s。"},
	WalletRemoved:                {English: "Removed %s.", Chinese: "已移除 %s。"},
	WalletRemoveTitle:            {English: "Remove wallet account?", Chinese: "移除钱包账户？"},
	WalletRemoveBody:             {English: "Remove %s? Its key is deleted from the keychain and the address stops being watched.", Chinese: "移除 %s？它的私钥会从钥匙串中删除，地址也不再被监视。"},
	WalletInvalidKey:             {English: "That is not a valid private key.", Chinese: "这不是有效的私钥。"},
	WalletDuplicate:              {English: "That account is already in the wallet.", Chinese: "该账户已在钱包中。"},
	WalletNoAccount:              {English: "That account is not in the wallet.", Chinese: "该账户不在钱包中。"},
	WalletKeychainFailed:         {English: "The system keychain refused: %s", Chinese: "系统钥匙串出错：%s"},
	WalletUnavailable:            {English: "The wallet is unavailable.", Chinese: "钱包不可用。"},
	WalletBadge:                  {English: "Wallet", Chinese: "钱包"},
	WalletEVMOnly:                {English: "Wallet accounts are EVM and NEAR accounts. Switch to an EVM or NEAR network to read their balances.", Chinese: "钱包账户是 EVM 与 NEAR 账户，切换到 EVM 或 NEAR 网络才能读取余额。"},
	WalletEVMSection:             {English: "EVM accounts", Chinese: "EVM 账户"},
	WalletNearSection:            {English: "NEAR accounts", Chinese: "NEAR 账户"},
	WalletNearNew:                {English: "New NEAR account", Chinese: "新建 NEAR 账户"},
	WalletNearBadge:              {English: "NEAR", Chinese: "NEAR"},
	WalletNearImport:             {English: "Import a NEAR key", Chinese: "导入 NEAR 私钥"},
	WalletNearKeyLabel:           {English: "ed25519 private key", Chinese: "ed25519 私钥"},
	WalletNearKeyPlaceholder:     {English: "ed25519:… or 64 hex characters", Chinese: "ed25519:… 或 64 位十六进制字符"},
	WalletNearAccountLabel:       {English: "Account ID (optional)", Chinese: "账户 ID（可选）"},
	WalletNearAccountPlaceholder: {English: "alice.testnet, or empty for an implicit account", Chinese: "alice.testnet；留空则使用隐式账户"},
	WalletNearImportNote:         {English: "An implicit account's id is its public key. A named account must be typed; the key alone does not name it.", Chinese: "隐式账户的 ID 就是公钥。命名账户必须手动填写，仅凭私钥无法确定它属于哪个账户。"},
	WalletNearCreated:            {English: "Created NEAR account %s. Fund it to activate it on chain.", Chinese: "已创建 NEAR 账户 %s。为其充值后才会在链上激活。"},
	WalletNearImported:           {English: "Imported NEAR account %s.", Chinese: "已导入 NEAR 账户 %s。"},
	WalletNearSendTitle:          {English: "Send NEAR", Chinese: "发送 NEAR"},
	WalletNearSendFrom:           {English: "From", Chinese: "从"},
	WalletNearSendTo:             {English: "Recipient", Chinese: "收款账户"},
	WalletNearSendToPlaceholder:  {English: "alice.testnet or a 64-hex account", Chinese: "alice.testnet 或 64 位十六进制账户"},
	WalletNearSendAmount:         {English: "Amount (NEAR)", Chinese: "数量（NEAR）"},
	WalletNearSendAmountHolder:   {English: "0.1", Chinese: "0.1"},
	WalletNearSend:               {English: "Review and send", Chinese: "检查并发送"},
	WalletNearSendNoNetwork:      {English: "Switch to a NEAR network to send.", Chinese: "切换到 NEAR 网络后才能发送。"},
	WalletNearSendNoAccount:      {English: "Add a NEAR account first.", Chinese: "请先添加一个 NEAR 账户。"},
	WalletNearSendLocked:         {English: "Unlock the account first.", Chinese: "请先解锁账户。"},
	WalletNearSendInvalidTo:      {English: "That is not a NEAR account ID.", Chinese: "这不是有效的 NEAR 账户 ID。"},
	WalletNearSendInvalidAmount:  {English: "Enter an amount of NEAR.", Chinese: "请输入 NEAR 数量。"},
	WalletNearSendSent:           {English: "Sent. Transaction %s", Chinese: "已发送。交易 %s"},
	WalletNearSendView:           {English: "View transaction", Chinese: "查看交易"},
	ApproveWalletWarning:         {English: "This transfer comes from your own wallet. Check the recipient and the amount before you approve.", Chinese: "这笔转账来自你自己的钱包。批准前请核对收款账户和金额。"},

	PasskeyTitle:          {English: "Passkey wallet", Chinese: "Passkey 钱包"},
	PasskeyBadge:          {English: "NEAR + EVM + Solana", Chinese: "NEAR + EVM + Solana"},
	PasskeyBody:           {English: "One passkey, held by your device or password manager, controls a NEAR account, an EVM smart account at the same address on every EVM chain, and a Solana vault.", Chinese: "一个由设备或密码管理器保管的 Passkey，控制一个 NEAR 账户、在所有 EVM 链上地址相同的 EVM 智能账户，以及一个 Solana 金库。"},
	PasskeyCreate:         {English: "Create passkey", Chinese: "创建 Passkey"},
	PasskeyWaiting:        {English: "Waiting for your passkey…", Chinese: "等待 Passkey 确认…"},
	PasskeyFine:           {English: "The private key never leaves the authenticator. Crypto Panel stores only the public key.", Chinese: "私钥不会离开认证器。Crypto Panel 只保存公钥。"},
	PasskeyCreated:        {English: "Passkey created. Its NEAR account can be funded. The Solana vault is not created on chain yet.", Chinese: "Passkey 已创建。其 NEAR 账户可以充值。Solana 金库尚未在链上创建。"},
	PasskeyFailed:         {English: "The passkey ceremony failed:", Chinese: "Passkey 流程失败："},
	PasskeyUnavailable:    {English: "Passkeys are unavailable in this build.", Chinese: "此版本无法使用 Passkey。"},
	PasskeyNearAccount:    {English: "NEAR account", Chinese: "NEAR 账户"},
	PasskeyEVMAccount:     {English: "EVM account", Chinese: "EVM 账户"},
	PasskeyEVMNote:        {English: "Sending from this smart account needs a bundler and is not wired.", Chinese: "从这个智能账户发送需要 bundler，当前尚未接入。"},
	PasskeySolanaAccount:  {English: "Solana vault", Chinese: "Solana 金库"},
	PasskeySolanaNote:     {English: "LazorKit v1 vault on Solana mainnet, derived from this passkey. Creating the account and sending SOL are not wired — do not send funds here yet.", Chinese: "由这把 Passkey 推导出的 Solana 主网 LazorKit v1 金库。创建账户和发送 SOL 都尚未接入——先不要往这里转账。"},
	PasskeyDeriving:       {English: "Reading from the factory…", Chinese: "正在从工厂合约读取…"},
	PasskeyMainnetOnly:    {English: "The NEAR passkey wallet contract is on mainnet. Switch to NEAR mainnet to create or send.", Chinese: "NEAR Passkey 钱包合约部署在主网。请切换到 NEAR 主网后再创建或发送。"},
	PasskeyDeploy:         {English: "Create account", Chinese: "创建账户"},
	PasskeyDeployHint:     {English: "The relayer pays the storage and gas; the passkey authorizes the account.", Chinese: "中继账户支付存储和 gas，Passkey 授权该账户。"},
	PasskeyDeployNote:     {English: "Create the passkey's NEAR account. %s relays the transaction and pays its fee; the passkey's key authorizes the account's StateInit.", Chinese: "创建 Passkey 的 NEAR 账户。%s 负责中继并支付手续费；Passkey 的密钥授权该账户的 StateInit。"},
	PasskeyDeployed:       {English: "Account created. Transaction %s", Chinese: "账户已创建。交易 %s"},
	PasskeyAlreadyOnChain: {English: "The account is already on chain.", Chinese: "该账户已经在链上了。"},
	PasskeySend:           {English: "Sign and send", Chinese: "签名并发送"},
	PasskeySendNote:       {English: "Send from the passkey account. %s relays the transaction and pays its fee; the passkey signs the transfer in your browser.", Chinese: "从 Passkey 账户发送。%s 负责中继并支付手续费；Passkey 会在浏览器中签署这笔转账。"},
	PasskeyNoRelayer:      {English: "Add and unlock a NEAR account first; it relays the passkey's transactions.", Chinese: "请先添加并解锁一个 NEAR 账户，它负责中继 Passkey 的交易。"},
	PasskeyRelayerLocked:  {English: "The relaying account was locked before it could sign.", Chinese: "中继账户在签名前被锁定了。"},
	PasskeyRejected:       {English: "The transaction was rejected. Nothing was signed or sent.", Chinese: "交易已被拒绝。没有签名，也没有发送任何东西。"},
	PasskeyForget:         {English: "Forget passkey", Chinese: "移除 Passkey"},
	PasskeyForgetTitle:    {English: "Forget this passkey?", Chinese: "移除此 Passkey？"},
	PasskeyForgetBody:     {English: "Crypto Panel will stop watching its accounts. The passkey itself stays in your device or password manager.", Chinese: "Crypto Panel 将不再监视它的账户。Passkey 本身仍保留在设备或密码管理器中。"},
	PasskeySettingsTitle:  {English: "Passkey wallet", Chinese: "Passkey 钱包"},
	PasskeySettingsBody:   {English: "Create a passkey that owns a NEAR account, an EVM smart account, and a Solana vault.", Chinese: "创建一个同时拥有 NEAR 账户、EVM 智能账户和 Solana 金库的 Passkey。"},
	PasskeySettingsHave:   {English: "Controlled by the passkey for %s, plus an EVM smart account and a Solana vault.", Chinese: "由 %s 的 Passkey 控制，同时还有一个 EVM 智能账户和一个 Solana 金库。"},

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
	ApproveDappKicker:     {English: "DApp · %s", Chinese: "DApp · %s"},
	ApproveSiteSays:       {English: "The site says", Chinese: "网站说"},
	ApproveSiteWords:      {English: "The site's words are not verified. The shell's decode above is what gets signed.", Chinese: "网站的话未经核实。上方由本应用解码的内容才是将要签名的内容。"},
	ApproveDappWarning:    {English: "Only approve if you trust this site and recognize this transaction.", Chinese: "只有在你信任该网站并认得这笔交易时才确认。"},

	ApproveReceiver:           {English: "Receiver", Chinese: "接收方"},
	ApproveSigningKey:         {English: "Signing key", Chinese: "签名密钥"},
	ApproveKeyLimit:           {English: "Key limit", Chinese: "密钥限额"},
	ApproveAmount:             {English: "Amount", Chinese: "金额"},
	ApproveYocto:              {English: "YoctoNEAR", Chinese: "YoctoNEAR"},
	ApproveAction:             {English: "Action", Chinese: "操作"},
	ApproveActionCall:         {English: "Function call", Chinese: "合约调用"},
	ApproveActionAddKey:       {English: "Add key", Chinese: "添加密钥"},
	ApproveActionDeleteKey:    {English: "Delete key", Chinese: "删除密钥"},
	ApproveActionStateInit:    {English: "Create deterministic account", Chinese: "创建确定性账户"},
	ApproveStateInitSize:      {English: "State init", Chinese: "状态初始化"},
	ApproveMethod:             {English: "Method", Chinese: "方法"},
	ApproveArgs:               {English: "Args", Chinese: "参数"},
	ApproveDeposit:            {English: "Deposit", Chinese: "附带押金"},
	ApproveGas:                {English: "Gas", Chinese: "Gas"},
	ApproveKey:                {English: "Key", Chinese: "密钥"},
	ApproveKeyContract:        {English: "Contract", Chinese: "合约"},
	ApproveKeyMethods:         {English: "Methods", Chinese: "方法"},
	ApproveKeyAllowance:       {English: "Allowance", Chinese: "额度"},
	ApproveKeyPermission:      {English: "Permission", Chinese: "权限"},
	ApproveFullAccess:         {English: "Full access", Chinese: "完全访问"},
	ApproveFullAccessKey:      {English: "Full access — no on-chain limit", Chinese: "完全访问 —— 链上无限制"},
	ApproveAllowanceOf:        {English: "up to %s", Chinese: "最多 %s"},
	ApproveAnyMethod:          {English: "any method", Chinese: "任意方法"},
	ApproveNoAllowance:        {English: "No limit", Chinese: "无上限"},
	ApproveNearCallTitle:      {English: "Call a contract?", Chinese: "调用合约？"},
	ApproveNearKeyTitle:       {English: "Add a key to this account?", Chinese: "为该账户添加密钥？"},
	ApproveNearDeleteKeyTitle: {English: "Delete a key?", Chinese: "删除密钥？"},
	ApproveNearTxTitle:        {English: "Sign this NEAR transaction?", Chinese: "签署这笔 NEAR 交易？"},

	NavIntents: {English: "Intents", Chinese: "跨链兑换"},

	IntentsSubtitle:             {English: "Cross-chain swaps, quoted by 1Click", Chinese: "由 1Click 报价的跨链兑换"},
	IntentsPayWith:              {English: "Pay with", Chinese: "付出"},
	IntentsReceive:              {English: "Receive", Chinese: "换回"},
	IntentsAmount:               {English: "Amount", Chinese: "数量"},
	IntentsAmountPlaceholder:    {English: "1.5", Chinese: "1.5"},
	IntentsRecipient:            {English: "Recipient", Chinese: "收款地址"},
	IntentsRecipientPlaceholder: {English: "An address on the destination chain", Chinese: "目标链上的地址"},
	IntentsRefund:               {English: "Refund to", Chinese: "退款到"},
	IntentsRefundPlaceholder:    {English: "An address on the origin chain", Chinese: "原链上的地址"},
	IntentsQuote:                {English: "Get a dry quote", Chinese: "获取 dry 报价"},
	IntentsQuoting:              {English: "Asking 1Click for a dry quote…", Chinese: "正在向 1Click 询价（dry）…"},
	IntentsLoadAssets:           {English: "Load supported assets", Chinese: "加载支持的资产"},
	IntentsLoadingAssets:        {English: "Loading the assets 1Click supports…", Chinese: "正在加载 1Click 支持的资产…"},
	IntentsAssetsNote:           {English: "%d assets from 1Click. A dry quote prices the swap and issues no deposit address: no funds move and nothing is signed.", Chinese: "1Click 支持 %d 种资产。dry 报价只给出价格，不生成存款地址：资金不会移动，也不会签名。"},
	IntentsNoAssets:             {English: "No assets are loaded yet.", Chinese: "尚未加载资产。"},
	IntentsPickAssets:           {English: "Choose the two assets first.", Chinese: "请先选择两种资产。"},
	IntentsAmountInvalid:        {English: "Enter an amount with at most %d decimals.", Chinese: "请输入最多 %d 位小数的数量。"},
	IntentsRecipientRequired:    {English: "Enter the address that should receive the swap.", Chinese: "请输入接收兑换的地址。"},
	IntentsRefundRequired:       {English: "Enter a refund address, or add a NEAR account to the wallet.", Chinese: "请输入退款地址，或在钱包里添加一个 NEAR 账户。"},
	IntentsNoClient:             {English: "The 1Click client is unavailable.", Chinese: "1Click 客户端不可用。"},
	IntentsRetry:                {English: "Try again", Chinese: "重试"},
	IntentsQuoteTitle:           {English: "Your dry quote", Chinese: "dry 报价"},
	IntentsDryBadge:             {English: "DRY RUN", Chinese: "DRY RUN"},
	IntentsYouPay:               {English: "You pay", Chinese: "你付出"},
	IntentsYouReceive:           {English: "You receive at least", Chinese: "至少换回"},
	IntentsEstimated:            {English: "Estimated receive", Chinese: "预计换回"},
	IntentsDeadline:             {English: "Deadline", Chinese: "期限"},
	IntentsDepositAddress:       {English: "Deposit address", Chinese: "存款地址"},
	IntentsNoDeposit:            {English: "None — a dry quote issues no deposit address", Chinese: "无 —— dry 报价不生成存款地址"},
	IntentsNoDeadline:           {English: "Not issued in a dry quote", Chinese: "dry 报价不生成期限"},
	IntentsRefundRow:            {English: "Refund to", Chinese: "退款到"},
	IntentsRecipientRow:         {English: "Recipient", Chinese: "收款地址"},
	IntentsReview:               {English: "Review in the approval window", Chinese: "在批准窗中查看"},
	IntentsRejected:             {English: "The quote was rejected. Nothing was signed or sent.", Chinese: "报价已被拒绝。没有签名，也没有发送任何东西。"},
	IntentsAccepted:             {English: "Dry run accepted. Nothing was signed or sent.", Chinese: "已接受 dry 报价。没有签名，也没有发送任何东西。"},
	IntentsQuotedBy:             {English: "Quoted by 1Click · %s", Chinese: "由 1Click 报价 · %s"},

	IntentsApproveWindowTitle: {English: "NEAR Intents", Chinese: "NEAR Intents"},
	IntentsApproveKicker:      {English: "NEAR INTENTS · DRY RUN", Chinese: "NEAR INTENTS · DRY RUN"},
	IntentsApproveTitle:       {English: "Swap via NEAR Intents?", Chinese: "通过 NEAR Intents 兑换？"},
	IntentsApprovePay:         {English: "You pay", Chinese: "你付出"},
	IntentsApproveReceive:     {English: "You receive at least", Chinese: "至少换回"},
	IntentsApproveDeadline:    {English: "Deadline", Chinese: "期限"},
	IntentsApproveDeposit:     {English: "Deposit", Chinese: "存款"},
	IntentsApproveIntent:      {English: "Intent", Chinese: "意图"},
	IntentsApproveRefund:      {English: "Refund to", Chinese: "退款到"},
	IntentsApproveRecipient:   {English: "Recipient", Chinese: "收款地址"},
	IntentsApproveSource:      {English: "Requested by", Chinese: "请求来自"},
	IntentsApproveSourceValue: {English: "Crypto Panel · dry run", Chinese: "Crypto Panel · dry 运行"},
	IntentsApproveWarning:     {English: "This is a dry quote: no funds move and nothing is signed. A real swap would need your explicit consent.", Chinese: "这是 dry 报价：资金不会移动，也不会签名。真正成交需要你明确同意。"},
	IntentsApproveAccept:      {English: "Accept quote", Chinese: "接受报价"},
	IntentsIntentText:         {English: "Swap %s %s on %s for at least %s %s on %s; refund to %s.", Chinese: "用 %s %s（%s）换至少 %s %s（%s）；退款到 %s。"},

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
	FiatTitle:       {English: "Fiat values", Chinese: "法币估值"},
	FiatBody: {English: "Show an approximate USD value from CoinGecko beside balances. Only asset ids are sent, never an address or a key.",
		Chinese: "在余额旁显示 CoinGecko 的美元估值。只发送资产 ID，不发送地址或私钥。"},
	FiatApprox:     {English: "≈ %s", Chinese: "约 %s"},
	Language:       {English: "Language", Chinese: "语言"},
	SystemLanguage: {English: "System", Chinese: "跟随系统"},
	NetworkSection: {English: "Network", Chinese: "网络"},
	AppSection:     {English: "App", Chinese: "应用"},
	About:          {English: "About", Chinese: "关于"},
	AboutBody:      {English: "Self-custody. Keys stay in the system keychain and never reach a plugin.", Chinese: "自托管。密钥保存在系统钥匙串中，插件永远无法触及。"},
	Version:        {English: "Version %s", Chinese: "版本 %s"},

	Seconds: {English: "%ds", Chinese: "%d 秒"},
	Minutes: {English: "%dm", Chinese: "%d 分钟"},

	PluginCount:         {English: "%d apps · %d on", Chinese: "%d 个应用 · %d 个已启用"},
	PluginInstall:       {English: "Install from folder…", Chinese: "从文件夹安装…"},
	PluginInstallTitle:  {English: "Choose an app folder", Chinese: "选择应用文件夹"},
	PluginInstalled:     {English: "Installed %s.", Chinese: "已安装 %s。"},
	PluginInstallFailed: {English: "Install failed: %s", Chinese: "安装失败：%s"},
	PluginEmpty:         {English: "No apps yet", Chinese: "还没有应用"},
	PluginEmptyWhy:      {English: "An app is a plugin: a folder with plugin.json and a JavaScript file. Install one from a folder, a zip, or the store.", Chinese: "应用就是插件：包含 plugin.json 和 JavaScript 文件的文件夹。可以从文件夹、zip 或商店安装。"},
	PluginOpenStore:     {English: "Open store", Chinese: "打开商店"},
	PluginUnsigned:      {English: "Unsigned", Chinese: "未签名"},
	PluginLogs:          {English: "Logs", Chinese: "日志"},
	PluginLogsEmpty:     {English: "Nothing logged yet.", Chinese: "还没有日志。"},
	PluginRemoveTitle:   {English: "Remove app?", Chinese: "移除应用？"},
	PluginRemoveBody:    {English: "Remove %s? Its folder and saved settings are deleted.", Chinese: "移除 %s？它的文件夹和已保存的设置都会被删除。"},
	PluginRemoved:       {English: "Removed %s.", Chinese: "已移除 %s。"},
	PluginRestarted:     {English: "Restarted %d×", Chinese: "已重启 %d 次"},
	PluginSettingsSaved: {English: "Settings saved.", Chinese: "设置已保存。"},
	PluginSave:          {English: "Save", Chinese: "保存"},
	PluginBack:          {English: "Back", Chinese: "返回"},
	PluginDisabled:      {English: "This app is turned off.", Chinese: "该应用已停用。"},
	AppOpen:             {English: "Open", Chinese: "打开"},
	AppSettings:         {English: "App settings", Chinese: "应用设置"},
	AppAll:              {English: "All apps", Chinese: "全部应用"},
	AppOff:              {English: "Off", Chinese: "已关闭"},
	AppNoPages:          {English: "This app has no page to open.", Chinese: "这个应用没有可打开的页面。"},
	AppBroken:           {English: "This app could not be loaded.", Chinese: "这个应用无法加载。"},
	AppZip:              {English: "Install from zip…", Chinese: "从 zip 安装…"},
	AppZipTitle:         {English: "Choose an app zip", Chinese: "选择应用 zip 文件"},
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
	StoreSourceZip:     {English: "a zip file", Chinese: "本地 zip 文件"},
	StoreSearch:        {English: "Search apps…", Chinese: "搜索应用…"},
	StoreNoMatches:     {English: "No app matches “%s”.", Chinese: "没有匹配“%s”的应用。"},
	StoreBy:            {English: "by %s", Chinese: "作者 %s"},
	StoreInstall:       {English: "Install", Chinese: "安装"},
	StoreUpdate:        {English: "Update", Chinese: "更新"},
	StoreInstalled:     {English: "Installed", Chinese: "已安装"},
	StoreInstalling:    {English: "Installing %s…", Chinese: "正在安装 %s…"},
	StoreUpdated:       {English: "Updated %s to v%s.", Chinese: "已将 %s 更新到 v%s。"},
	StoreFailed:        {English: "Install failed: %s", Chinese: "安装失败：%s"},
	StoreCanceled:      {English: "Install canceled.", Chinese: "已取消安装。"},

	AgentSection:     {English: "Local API", Chinese: "本地 API"},
	AgentTitle:       {English: "Local agent API", Chinese: "本地 Agent API"},
	AgentBody:        {English: "A read-only API on this machine for a local agent: the portfolio and the installed apps. It cannot sign or approve, and it listens on loopback only.", Chinese: "供本机 Agent 读取的只读接口：持仓和已安装应用。不能签名或批准，且只监听本机回环地址。"},
	AgentPort:        {English: "Port", Chinese: "端口"},
	AgentPortInvalid: {English: "The port must be 1 to 65535.", Chinese: "端口必须是 1 到 65535。"},
	AgentListening:   {English: "Listening on http://%s", Chinese: "正在监听 http://%s"},
	AgentToken:       {English: "Bearer token", Chinese: "访问令牌"},
	AgentRotate:      {English: "Rotate token", Chinese: "更换令牌"},
	AgentCurl:        {English: "curl -H \"Authorization: Bearer <token>\" http://%s/v0/portfolio", Chinese: "curl -H \"Authorization: Bearer <token>\" http://%s/v0/portfolio"},

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
