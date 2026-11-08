// Package featured is the plugin store's built-in catalog: two signed
// example plugins and the registry that lists them. It is embedded in the
// app, so the store works before the user points it at a registry of their
// own.
package featured

import (
	"embed"
	"strings"
)

//go:embed registry.json *.zip
var files embed.FS

// Registry returns the built-in catalog.
func Registry() []byte {
	data, err := files.ReadFile("registry.json")
	if err != nil {
		panic("featured: " + err.Error())
	}
	return data
}

// Archive returns the built-in archive an entry's URL names, when it is one
// of the catalog's own.
func Archive(rawURL string) ([]byte, bool) {
	name, ok := strings.CutPrefix(rawURL, "embedded:")
	if !ok || name == "" || strings.ContainsAny(name, `/\`) {
		return nil, false
	}
	data, err := files.ReadFile(name)
	if err != nil {
		return nil, false
	}
	return data, true
}
