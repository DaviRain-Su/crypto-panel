// Package plugin is the plugin host: the manifest schema, the store of
// installed plugins, the moejs runtime each plugin runs in, and the host
// functions its manifest grants. Plugins are untrusted; every capability
// they have is a function this package installs, and nothing else exists.
package plugin

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/version"
)

// Manifest is plugin.json: what a plugin is and what it may do. The schema
// is strict — an unknown key fails the install rather than being ignored
// (design §8). Signing is not here: a manifest cannot sign itself, so a
// store install's hash and signature live in the registry entry and in the
// shell's install.json.
type Manifest struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Version       string         `json:"version"`
	MinAppVersion string         `json:"minAppVersion,omitempty"`
	Main          string         `json:"main"`
	Description   string         `json:"description,omitempty"`
	Author        string         `json:"author,omitempty"`
	Permissions   Permissions    `json:"permissions"`
	Settings      []SettingField `json:"settings,omitempty"`
	Navigation    []NavItem      `json:"navigation,omitempty"`
}

// Permissions is what a plugin may reach. Unknown keys are an install
// error, so a typo cannot silently widen or narrow a plugin.
type Permissions struct {
	// Network is the fetch allowlist: hostnames, or "*.example.com" for
	// every subdomain.
	Network []string `json:"network,omitempty"`
	// Chains are the CAIP-2 ids host.rpc may name.
	Chains []string `json:"chains,omitempty"`
	// Read lists the shell data host.accounts may return: "balances".
	Read []string `json:"read,omitempty"`
	// Sign asks for host.requestSign: the plugin may ask the shell to sign,
	// and every request still passes the shell's own decoded approval.
	Sign bool `json:"sign,omitempty"`
	// StorageKb is the host.kv quota in kilobytes.
	StorageKb int `json:"storageKb,omitempty"`
}

// SettingField is one row of the plugin's settings form.
type SettingField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Secret 始终序列化：设置状态接口的客户端按键存在性判断属性。
	Secret bool   `json:"secret"`
	Help   string `json:"help,omitempty"`
	Link   string `json:"link,omitempty"`
}

// NavItem is one page the plugin adds to the app.
type NavItem struct {
	Title string `json:"title"`
	Page  string `json:"page"`
	Icon  string `json:"icon,omitempty"`
}

const (
	maxManifestBytes = 64 << 10
	maxSourceBytes   = 1 << 20
	maxSettings      = 20
	maxNavigation    = 8
	maxStorageKb     = 1024
)

var (
	idPattern    = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z0-9][a-z0-9-]*)+$`)
	keyPattern   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)
	pagePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	hostPattern  = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)
	versionRegex = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)
)

// Readable data a plugin may ask host.accounts for.
var readable = map[string]bool{"balances": true}

// ParseManifest decodes and validates plugin.json.
func ParseManifest(data []byte) (Manifest, error) {
	if len(data) > maxManifestBytes {
		return Manifest{}, errors.New("plugin.json: manifest is larger than 65536 bytes")
	}
	var m Manifest
	if err := DecodeJSON(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("plugin.json: %w", err)
	}
	if err := m.validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// LoadManifest reads and validates dir/plugin.json.
func LoadManifest(dir string) (Manifest, error) {
	p := filepath.Join(dir, "plugin.json")
	info, err := os.Stat(p)
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin.json: %w", err)
	}
	if info.Size() > maxManifestBytes {
		return Manifest{}, fmt.Errorf("plugin.json: larger than %d bytes", maxManifestBytes)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin.json: %w", err)
	}
	return ParseManifest(data)
}

func (m Manifest) validate() error {
	if !idPattern.MatchString(m.ID) || len(m.ID) > 128 {
		return errors.New(`plugin.json: "id" must be reverse-DNS, lowercase, such as "com.example.portfolio"`)
	}
	if strings.TrimSpace(m.Name) == "" || len([]rune(m.Name)) > 64 {
		return errors.New(`plugin.json: "name" must be 1 to 64 characters`)
	}
	if !versionRegex.MatchString(m.Version) {
		return fmt.Errorf("plugin.json: version %q is not semver (x.y.z)", m.Version)
	}
	if m.MinAppVersion != "" {
		if !versionRegex.MatchString(m.MinAppVersion) {
			return fmt.Errorf("plugin.json: minAppVersion %q is not semver (x.y.z)", m.MinAppVersion)
		}
		if compareVersions(m.MinAppVersion, version.Version) > 0 {
			return fmt.Errorf("plugin.json: needs app %s, this is %s", m.MinAppVersion, version.Version)
		}
	}
	if err := validateMain(m.Main); err != nil {
		return err
	}
	if len([]rune(m.Description)) > 400 {
		return errors.New(`plugin.json: "description" is longer than 400 characters`)
	}
	if len(m.Settings) > maxSettings {
		return fmt.Errorf("plugin.json: more than %d settings", maxSettings)
	}
	seenSettings := make(map[string]bool, len(m.Settings))
	for _, s := range m.Settings {
		if seenSettings[s.Key] {
			return errors.New("plugin.json: duplicate setting key")
		}
		seenSettings[s.Key] = true
		if err := validateSetting(s); err != nil {
			return err
		}
	}
	if len(m.Navigation) > maxNavigation {
		return fmt.Errorf("plugin.json: more than %d navigation entries", maxNavigation)
	}
	seenPages := make(map[string]bool, len(m.Navigation))
	for _, n := range m.Navigation {
		if seenPages[n.Page] {
			return errors.New("plugin.json: duplicate navigation page")
		}
		seenPages[n.Page] = true
		if !safeMetadata(n.Title) || !safeMetadata(n.Icon) {
			return errors.New("plugin.json: unsafe navigation text")
		}
		if strings.TrimSpace(n.Title) == "" || len([]rune(n.Title)) > 40 {
			return errors.New(`plugin.json: a navigation title must be 1 to 40 characters`)
		}
		if !pagePattern.MatchString(n.Page) {
			return fmt.Errorf("plugin.json: navigation page %q must be lowercase letters, digits and dashes", n.Page)
		}
		if len(n.Icon) > 32 {
			return errors.New(`plugin.json: a navigation icon is longer than 32 characters`)
		}
	}
	if !safeMetadata(m.Name) || !safeMetadata(m.Description) || !safeMetadata(m.Author) || len([]rune(m.Author)) > 80 {
		return errors.New("plugin.json: unsafe or oversized manifest text")
	}
	return m.Permissions.validate()
}

func validateMain(main string) error {
	if main == "" {
		return errors.New(`plugin.json: "main" is required`)
	}
	if path.IsAbs(main) || strings.Contains(main, `\`) {
		return errors.New(`plugin.json: "main" must be a relative path with forward slashes`)
	}
	clean := path.Clean(main)
	if clean != main || clean == "." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("plugin.json: main %q must be a clean path inside the plugin", main)
	}
	switch path.Ext(main) {
	case ".js", ".mjs":
	default:
		return fmt.Errorf("plugin.json: main %q must be a .js or .mjs module", main)
	}
	return nil
}

func validateSetting(s SettingField) error {
	if !safeMetadata(s.Label) || !safeMetadata(s.Help) || !safeMetadata(s.Link) {
		return errors.New("plugin.json: unsafe setting text")
	}
	if !keyPattern.MatchString(s.Key) {
		return fmt.Errorf("plugin.json: setting key %q must be letters, digits, _ or -", s.Key)
	}
	if strings.TrimSpace(s.Label) == "" || len([]rune(s.Label)) > 80 {
		return fmt.Errorf("plugin.json: setting %q needs a label of 1 to 80 characters", s.Key)
	}
	if len([]rune(s.Help)) > 200 {
		return fmt.Errorf("plugin.json: setting %q help is longer than 200 characters", s.Key)
	}
	if s.Link != "" {
		u, err := url.Parse(s.Link)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("plugin.json: setting %q link must be an https URL", s.Key)
		}
	}
	return nil
}

func (p Permissions) validate() error {
	for _, host := range p.Network {
		if !hostPattern.MatchString(strings.ToLower(host)) {
			return fmt.Errorf("plugin.json: network entry %q must be a hostname such as api.example.com or *.example.com", host)
		}
	}
	for _, id := range p.Chains {
		if _, ok := chains.ByID(id); !ok {
			return fmt.Errorf("plugin.json: chain %q is not one this app knows", id)
		}
	}
	for _, r := range p.Read {
		if !readable[r] {
			return fmt.Errorf("plugin.json: read scope %q is not one this app has", r)
		}
	}
	if p.Sign && len(p.Chains) == 0 {
		return errors.New(`plugin.json: "sign" needs at least one chain in permissions.chains`)
	}
	if p.StorageKb < 0 || p.StorageKb > maxStorageKb {
		return fmt.Errorf("plugin.json: storageKb must be between 0 and %d", maxStorageKb)
	}
	return nil
}

// ValidateDir checks the files the manifest names exist and are sane. It
// runs after validate, so the paths are already clean.
func (m Manifest) ValidateDir(dir string) error {
	p := filepath.Join(dir, filepath.FromSlash(m.Main))
	info, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("plugin.json: main %q: %w", m.Main, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("plugin.json: main %q is not a regular file", m.Main)
	}
	if info.Size() > maxSourceBytes {
		return fmt.Errorf("plugin.json: main %q is larger than %d bytes", m.Main, maxSourceBytes)
	}
	return nil
}

// compareVersions orders two x.y.z versions, a prerelease before its
// release. It returns -1, 0 or 1.
func compareVersions(a, b string) int {
	acore, apre, _ := strings.Cut(a, "-")
	bcore, bpre, _ := strings.Cut(b, "-")
	an, bn := strings.Split(acore, "."), strings.Split(bcore, ".")
	for i := 0; i < len(an) && i < len(bn); i++ {
		if cmp := compareDecimal(an[i], bn[i]); cmp != 0 {
			return cmp
		}
	}
	if apre == "" && bpre == "" {
		return 0
	}
	if apre == "" {
		return 1
	}
	if bpre == "" {
		return -1
	}
	aparts, bparts := strings.Split(apre, "."), strings.Split(bpre, ".")
	for i := 0; i < len(aparts) && i < len(bparts); i++ {
		av, bv := aparts[i], bparts[i]
		anum, bnum := decimalIdentifier(av), decimalIdentifier(bv)
		switch {
		case anum && bnum:
			if cmp := compareDecimal(av, bv); cmp != 0 {
				return cmp
			}
		case anum:
			return -1
		case bnum:
			return 1
		default:
			if cmp := strings.Compare(av, bv); cmp != 0 {
				return cmp
			}
		}
	}
	if len(aparts) < len(bparts) {
		return -1
	}
	if len(aparts) > len(bparts) {
		return 1
	}
	return 0
}

// 用十进制字符串比较，避免攻击者提供超大版本号造成整数溢出。
func compareDecimal(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}
func decimalIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
