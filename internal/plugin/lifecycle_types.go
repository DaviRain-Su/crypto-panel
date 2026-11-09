package plugin

import (
	"encoding/json"
	"time"
)

// Package 是与两文件目录等价的单文件应用包。
type Package struct {
	Plugin json.RawMessage `json:"plugin"`
	Main   string          `json:"main"`
}

// Source 指定一种候选来源；三种形式不可混用。
type Source struct {
	Kind    string   `json:"kind"`
	Path    string   `json:"path,omitempty"`
	Package *Package `json:"package,omitempty"`
}

// Error 是可供管理接口映射的安全错误，不携带底层输入或路径。
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *Error) Error() string { return e.Message }

func managementError(code, message string, status int) *Error {
	return &Error{Code: code, Message: message, Status: status}
}

// ValidationError 只返回字段位置、稳定错误码与安全文案。
type ValidationError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidationResult 不为无效应用返回可信摘要。
type ValidationResult struct {
	Valid       bool              `json:"valid"`
	PackageHash string            `json:"packageHash,omitempty"`
	Summary     *Manifest         `json:"summary,omitempty"`
	Errors      []ValidationError `json:"errors"`
}

// Installation 是可供原生权限卡与管理接口使用的安全任务记录。
type Installation struct {
	RequestID    string    `json:"requestId"`
	Operation    string    `json:"operation"`
	PluginID     string    `json:"pluginId"`
	Version      string    `json:"version"`
	PackageHash  string    `json:"packageHash"`
	ExpectedHash string    `json:"expectedHash,omitempty"`
	Status       string    `json:"status"`
	Via          string    `json:"via"`
	Manifest     Manifest  `json:"manifest"`
	OldManifest  *Manifest `json:"oldManifest,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
	CompletedAt  time.Time `json:"completedAt,omitzero"`
	Code         string    `json:"code,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// SettingStatus 对秘密字段只公布是否配置，永远不携带其值。
type SettingStatus struct {
	SettingField
	Value      *string `json:"value,omitempty"`
	Configured *bool   `json:"configured,omitempty"`
}

// SettingsWriteError 只描述部分写入结果，不暴露凭据服务原始错误。
type SettingsWriteError struct {
	AppliedKeys []string `json:"appliedKeys"`
	FailedKeys  []string `json:"failedKeys"`
}

func (e *SettingsWriteError) Error() string { return "设置写入失败，请根据键名重试" }

const (
	MaxPackageBytes         = 12 << 20
	MaxSettingValueBytes    = 8192
	MaxSettingsBytes        = 1 << 20
	MaxPendingInstallations = 5
	InstallationTTL         = 30 * time.Minute
	MaxInstallationHistory  = 100
	InstallationHistoryTTL  = 24 * time.Hour
)
