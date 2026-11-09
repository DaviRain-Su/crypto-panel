package i18n

// pluginErrorKeys maps the plugin layer's stable error codes to their messages.
// The plugin package builds one message per code for every caller, so the UI
// and the local API translate at their own boundary instead of displaying the
// plugin layer's internal text.
var pluginErrorKeys = map[string]Key{
	"APP_BUSY":                    PluginErrAppBusy,
	"BASE_CHANGED":                PluginErrBaseChanged,
	"BROKEN_PLUGIN":               PluginErrBroken,
	"CANDIDATE_CHANGED":           PluginErrCandidateChanged,
	"CANDIDATE_CONFLICT":          PluginErrCandidateConflict,
	"CREDENTIALS_UNAVAILABLE":     PluginErrCredentialsUnavailable,
	"EXAMPLE_NOT_FOUND":           PluginErrExampleNotFound,
	"EXAMPLE_UNAVAILABLE":         PluginErrExampleUnavailable,
	"EXPECTED_HASH_REQUIRED":      PluginErrExpectedHashRequired,
	"HASH_UNAVAILABLE":            PluginErrHashUnavailable,
	"INSTALLATION_FAILED":         PluginErrInstallationFailed,
	"INSTALLATION_NOT_FOUND":      PluginErrInstallationNotFound,
	"INSTALLATION_NOT_PENDING":    PluginErrInstallationNotPending,
	"INVALID_EXPECTED_HASH":       PluginErrInvalidExpectedHash,
	"INVALID_OPERATION":           PluginErrInvalidOperation,
	"INVALID_PLUGIN_DIR":          PluginErrInvalidDir,
	"INVALID_PLUGIN_ID":           PluginErrInvalidID,
	"INVALID_SOURCE":              PluginErrInvalidSource,
	"INVALID_STORE":               PluginErrInvalidStore,
	"INVALID_VIA":                 PluginErrInvalidVia,
	"MANAGER_CLOSED":              PluginErrManagerClosed,
	"PACKAGE_TOO_LARGE":           PluginErrPackageTooLarge,
	"PERSONAL_NAMESPACE_REQUIRED": PluginErrPersonalNamespace,
	"PLUGIN_EXISTS":               PluginErrExists,
	"PLUGIN_ID_MISMATCH":          PluginErrIDMismatch,
	"PLUGIN_NOT_FOUND":            PluginErrNotFound,
	"PROPOSAL_CANCELLED":          PluginErrProposalCancelled,
	"QUEUE_FULL":                  PluginErrQueueFull,
	"RECOVERY_FAILED":             PluginErrRecoveryFailed,
	"RECOVERY_REQUIRED":           PluginErrRecoveryRequired,
	"REMOVE_FAILED":               PluginErrRemoveFailed,
	"REQUEST_ID_UNAVAILABLE":      PluginErrRequestID,
	"SETTINGS_MIGRATION_REQUIRED": PluginErrSettingsMigration,
	"SETTINGS_TOO_LARGE":          PluginErrSettingsTooLarge,
	"SETTINGS_UNAVAILABLE":        PluginErrSettingsUnavailable,
	"SETTINGS_WRITE_FAILED":       PluginErrSettingsWriteFailed,
	"SETTING_TOO_LARGE":           PluginErrSettingTooLarge,
	"SOURCE_NOT_ALLOWED":          PluginErrSourceNotAllowed,
	"SOURCE_NOT_FOUND":            PluginErrSourceNotFound,
	"SOURCE_UNAVAILABLE":          PluginErrSourceUnavailable,
	"SOURCE_UNREADABLE":           PluginErrSourceUnreadable,
	"STORAGE_QUOTA_CONFLICT":      PluginErrStorageQuota,
	"STORE_UNAVAILABLE":           PluginErrStoreUnavailable,
	"TARGET_REQUIRED":             PluginErrTargetRequired,
	"UNDECLARED_SETTING":          PluginErrUndeclaredSetting,
	"VALIDATION_FAILED":           PluginErrValidationFailed,
	"VERSION_CONFLICT":            PluginErrVersionConflict,
}

// PluginErrorKey returns the message key for a plugin error code.
func PluginErrorKey(code string) (Key, bool) {
	key, ok := pluginErrorKeys[code]
	return key, ok
}
