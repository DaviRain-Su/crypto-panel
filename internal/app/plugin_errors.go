package app

import (
	"errors"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/plugin"
)

// pluginErrorText localizes a plugin-layer error by its stable code. The
// plugin layer builds one message per code for every caller, so the UI
// translates at this boundary instead of showing that internal text.
func (a *App) pluginErrorText(err error) string {
	var partial *plugin.SettingsWriteError
	if errors.As(err, &partial) {
		return a.t(i18n.PluginSettingsWriteFailed)
	}
	var known *plugin.Error
	if errors.As(err, &known) {
		if key, ok := i18n.PluginErrorKey(known.Code); ok {
			return a.t(key)
		}
	}
	return err.Error()
}
