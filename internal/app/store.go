package app

import (
	"context"
	"strings"
	"time"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/plugin"
)

// The plugin store's actions. A catalog is a static JSON file listing signed
// archives; an install downloads one, checks it against its entry, asks the
// user what it may do, and only then moves it into the plugins folder.

// installRequest is what the permission prompt needs beyond the manifest.
type installRequest struct {
	update   bool
	old      *plugin.Manifest
	unsigned bool
	source   string
}

// setPluginTab switches the Plugins page between the installed plugins and
// the store.
func (a *App) setPluginTab(tab string) {
	if a.pluginTab == tab {
		return
	}
	a.pluginTab = tab
	if tab == "store" && !a.storeLoaded && !a.storeLoading {
		a.loadStore()
	}
	a.invalidate()
}

// loadStore reads the catalog the user's registry URL names. With no URL
// there is no catalog: the store shows how to set one. The fetch runs off
// the main thread; the result comes back through runOnMain, and an answer
// from before a change is dropped.
func (a *App) loadStore() {
	if a.plugins == nil {
		return
	}
	a.storeGen++
	gen := a.storeGen
	a.storeLoading = true
	a.storeErr = ""
	rawURL := a.settings.PluginStoreURL
	if rawURL == "" {
		a.storeLoading = false
		a.storeItems = nil
		a.storeLoaded = true
		a.invalidate()
		return
	}
	a.invalidate()
	a.goAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		reg, err := plugin.FetchRegistry(ctx, plugin.DefaultClient(), rawURL)
		a.runOnMain(func() {
			if gen != a.storeGen {
				return
			}
			a.storeLoading = false
			if err != nil {
				a.storeErr = err.Error()
				a.invalidate()
				return
			}
			a.storeItems = reg.Plugins
			a.storeLoaded = true
			a.invalidate()
		})
	})
}

// storeSourceName names the catalog in the store's footer and in the
// install prompt.
func (a *App) storeSourceName() string {
	return a.settings.PluginStoreURL
}

// installedPlugin returns the installed plugin with id, if there is one.
func (a *App) installedPlugin(id string) (plugin.Installed, bool) {
	for _, item := range a.pluginItems {
		if item.Manifest.ID == id {
			return item, true
		}
	}
	return plugin.Installed{}, false
}

// installFromStore downloads one entry, verifies it, asks for permission and
// installs it. The download and the unpacking run off the main thread; the
// prompt and the move into place run on it.
func (a *App) installFromStore(entry plugin.RegistryEntry) {
	if a.plugins == nil || a.storeInstalling != "" {
		return
	}
	a.storeInstalling = entry.ID
	a.pluginNotice = a.t(i18n.StoreInstalling, entry.Name)
	a.invalidate()
	a.goAsync(func() {
		data, err := a.fetchArchive(entry)
		var ar *plugin.Archive
		if err == nil {
			ar, err = a.plugins.Prepare(data, entry)
		}
		if err != nil {
			a.runOnMain(func() {
				if a.storeInstalling != entry.ID {
					return
				}
				a.storeInstalling = ""
				a.pluginNotice = a.t(i18n.StoreFailed, err.Error())
				a.invalidate()
			})
			return
		}
		a.runOnMain(func() {
			if a.storeInstalling != entry.ID {
				ar.Close()
				return
			}
			req := installRequest{source: a.storeSourceName()}
			if old, ok := a.installedPlugin(entry.ID); ok {
				req.update = true
				req.old = &old.Manifest
			}
			a.confirmInstall(ar.Manifest, req, func(ok bool) {
				a.storeInstalling = ""
				if !ok {
					ar.Close()
					a.pluginNotice = a.t(i18n.StoreCanceled)
					a.invalidate()
					return
				}
				a.stopRunner(entry.ID)
				m, err := ar.Install()
				if err != nil {
					ar.Close()
					a.pluginNotice = a.t(i18n.StoreFailed, err.Error())
					a.invalidate()
					return
				}
				if req.update {
					a.pluginNotice = a.t(i18n.StoreUpdated, m.Name, m.Version)
				} else {
					a.pluginNotice = a.t(i18n.PluginInstalled, m.Name)
				}
				a.refreshPlugins()
				if a.route.kind == "plugin" && a.route.id == m.ID {
					a.loadPluginPage()
				}
				a.invalidate()
			})
		})
	})
}

// fetchArchive reads an entry's archive from the network. The hash and
// signature are checked by Prepare, not here.
func (a *App) fetchArchive(entry plugin.RegistryEntry) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return plugin.FetchArchive(ctx, plugin.DefaultClient(), entry.URL)
}

// applyStoreURL saves the registry URL and reads it again.
func (a *App) applyStoreURL() {
	raw := strings.TrimSpace(a.storeURLDraft)
	if raw != "" && !plugin.ValidRegistryURL(raw) {
		a.storeFormErr = a.t(i18n.StoreInvalidURL)
		a.invalidate()
		return
	}
	a.settings.PluginStoreURL = raw
	a.settings = a.settings.Validate()
	a.storeURLDraft = a.settings.PluginStoreURL
	a.save()
	a.storeFormErr = ""
	a.resetStore()
	a.invalidate()
}

// resetStoreURL goes back to the built-in catalog.
func (a *App) resetStoreURL() {
	a.storeURLDraft = ""
	a.settings.PluginStoreURL = ""
	a.settings = a.settings.Validate()
	a.save()
	a.storeFormErr = ""
	a.resetStore()
	a.invalidate()
}

// resetStore drops the catalog that was read, so the next look fetches the
// new source.
func (a *App) resetStore() {
	a.storeGen++
	a.storeItems = nil
	a.storeErr = ""
	a.storeLoading = false
	a.storeLoaded = false
	if a.pluginTab == "store" {
		a.loadStore()
	}
}
