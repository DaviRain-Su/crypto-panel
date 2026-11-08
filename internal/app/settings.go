package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/plugin"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// Settings is everything the shell keeps between launches.
type Settings struct {
	// NetworkID is the CAIP-2 identifier of the watched network.
	NetworkID string `json:"networkId,omitempty"`
	// ChainID is the M0 field, read once to migrate an old file and never
	// written again.
	ChainID        uint64 `json:"chainId,omitempty"`
	RPCURL         string `json:"rpcUrl,omitempty"`
	Language       string `json:"language,omitempty"`
	RefreshSeconds int    `json:"refreshSeconds,omitempty"`
	// PluginStoreURL is the registry the store reads. Empty means the
	// app's built-in catalog.
	PluginStoreURL string           `json:"pluginStoreUrl,omitempty"`
	Accounts       []wallet.Address `json:"accounts"`
	// Wallet is the native wallet's accounts: addresses and labels only. The
	// keys are in the OS keychain, never in this file.
	Wallet []wallet.Account `json:"wallet,omitempty"`
}

const (
	// DefaultRefreshSeconds is how often balances are read again.
	DefaultRefreshSeconds = 60
	// MinRefreshSeconds keeps a public endpoint from being polled too hard.
	MinRefreshSeconds = 15
	// MaxRefreshSeconds is the slowest the settings offer.
	MaxRefreshSeconds = 3600
)

// DefaultSettings is the state before the user changes anything.
func DefaultSettings() Settings {
	return Settings{
		NetworkID:      chains.Default().ID,
		RefreshSeconds: DefaultRefreshSeconds,
	}
}

// LoadSettings reads path, falling back to defaults when it does not exist.
// A file that cannot be decoded is an error the caller may show, but the
// returned settings are still usable defaults. An M0 file that names a
// chain id is migrated to the matching network id.
func LoadSettings(path string) (Settings, error) {
	s := DefaultSettings()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s.Validate(), nil
	}
	if err != nil {
		return s.Validate(), err
	}
	// Decode into a zero value, so a missing field stays distinguishable
	// from a default: an M0 file has no networkId, only a chainId.
	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		return DefaultSettings().Validate(), fmt.Errorf("settings %s: %w", path, err)
	}
	if loaded.NetworkID == "" {
		if n, ok := legacyNetwork(loaded.ChainID); ok {
			loaded.NetworkID = n.ID
		}
	}
	return loaded.Validate(), nil
}

// legacyNetwork maps an M0 chain id to its preset.
func legacyNetwork(id uint64) (chain.Network, bool) {
	for _, n := range chains.All() {
		if n.Family == chain.EVM && n.ChainID == id {
			return n, true
		}
	}
	return chain.Network{}, false
}

// SaveSettings writes s to path atomically: a crash leaves either the old
// file or the new one, never half of one.
func SaveSettings(path string, s Settings) error {
	s = s.Validate()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Validate returns s with every field in range: a known network, a usable
// endpoint, a language the app has, a sane interval, and addresses that are
// valid, normalized, unique and labelled within limits. Addresses of both
// families are kept: switching networks is how they become visible. The
// wallet's accounts are watched too, so they are not repeated in Accounts.
func (s Settings) Validate() Settings {
	if _, ok := chains.ByID(s.NetworkID); !ok {
		s.NetworkID = chains.Default().ID
		s.RPCURL = ""
	}
	s.ChainID = 0
	if s.RPCURL != "" && !ValidRPCURL(s.RPCURL) {
		s.RPCURL = ""
	}
	if s.PluginStoreURL != "" && !plugin.ValidRegistryURL(s.PluginStoreURL) {
		s.PluginStoreURL = ""
	}
	switch s.Language {
	case i18n.System, i18n.English, i18n.Chinese:
	default:
		s.Language = i18n.System
	}
	if s.RefreshSeconds < MinRefreshSeconds || s.RefreshSeconds > MaxRefreshSeconds {
		s.RefreshSeconds = DefaultRefreshSeconds
	}
	seen := make(map[string]bool, len(s.Accounts))
	accounts := make([]wallet.Address, 0, len(s.Accounts))
	for _, a := range s.Accounts {
		kind, ok := wallet.KindOf(a.Addr)
		if !ok {
			continue
		}
		addr, err := wallet.Normalize(kind, a.Addr)
		if err != nil || seen[addr] {
			continue
		}
		seen[addr] = true
		accounts = append(accounts, wallet.Address{Addr: addr, Label: trimLabel(a.Label)})
	}
	// The wallet holds EVM accounts for now; Solana signing comes later.
	seenWallet := make(map[string]bool, len(s.Wallet))
	walletAccounts := make([]wallet.Account, 0, len(s.Wallet))
	for _, a := range s.Wallet {
		addr, err := wallet.Normalize(wallet.EVM, a.Addr)
		if err != nil || seenWallet[addr] {
			continue
		}
		seenWallet[addr] = true
		walletAccounts = append(walletAccounts, wallet.Account{Addr: addr, Label: trimLabel(a.Label)})
	}
	kept := accounts[:0]
	for _, a := range accounts {
		if seenWallet[a.Addr] {
			continue
		}
		kept = append(kept, a)
	}
	s.Accounts = kept
	s.Wallet = walletAccounts
	return s
}

// trimLabel keeps a label to the length a row can show.
func trimLabel(label string) string {
	label = strings.TrimSpace(label)
	if r := []rune(label); len(r) > 40 {
		label = string(r[:40])
	}
	return label
}

// ValidRPCURL reports an http(s) URL with a host.
func ValidRPCURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}

// Network returns the preset s names.
func (s Settings) Network() chain.Network {
	n, ok := chains.ByID(s.NetworkID)
	if !ok {
		return chains.Default()
	}
	return n
}

// Reader builds the reader for the selected network, at the custom endpoint
// when one is set.
func (s Settings) Reader() (chain.Reader, error) {
	return chains.NewReader(s.Network(), s.RPCURL)
}

// Has reports whether addr is already watched, by the watch list or the
// wallet.
func (s Settings) Has(addr string) bool {
	for _, a := range s.Accounts {
		if a.Addr == addr {
			return true
		}
	}
	return s.IsWallet(addr)
}

// IsWallet reports whether addr is an account of the native wallet.
func (s Settings) IsWallet(addr string) bool {
	for _, a := range s.Wallet {
		if a.Addr == addr {
			return true
		}
	}
	return false
}

// WatchedOf lists every address of one family the app reads: the watch list
// and, on EVM, the wallet's own accounts.
func (s Settings) WatchedOf(family chain.Family) []wallet.Address {
	out := s.AccountsOf(family)
	if family != chain.EVM {
		return out
	}
	seen := make(map[string]bool, len(out)+len(s.Wallet))
	for _, a := range out {
		seen[a.Addr] = true
	}
	for _, a := range s.Wallet {
		if seen[a.Addr] {
			continue
		}
		seen[a.Addr] = true
		out = append(out, wallet.Address{Addr: a.Addr, Label: a.Label})
	}
	return out
}

// AccountsOf lists the watched addresses of one family, in stored order.
func (s Settings) AccountsOf(family chain.Family) []wallet.Address {
	out := make([]wallet.Address, 0, len(s.Accounts))
	for _, a := range s.Accounts {
		if f, ok := addressFamily(a.Addr); ok && f == family {
			out = append(out, a)
		}
	}
	return out
}

// addressFamily maps an address to the family that can read it.
func addressFamily(addr string) (chain.Family, bool) {
	kind, ok := wallet.KindOf(addr)
	if !ok {
		return "", false
	}
	switch kind {
	case wallet.EVM:
		return chain.EVM, true
	case wallet.Solana:
		return chain.Solana, true
	}
	return "", false
}
