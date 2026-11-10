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

	"github.com/davirain-su/lintel/internal/agent/authoring"
	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/neartx"
	"github.com/davirain-su/lintel/internal/plugin"
	"github.com/davirain-su/lintel/internal/wallet"
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
	// PluginStoreURL is the registry the store reads. Empty means no store.
	PluginStoreURL string `json:"pluginStoreUrl,omitempty"`
	// AgentEnabled 开启本机 API；Bearer token 仅保存在系统凭据服务。
	AgentEnabled bool `json:"agentEnabled,omitempty"`
	// AgentPort is the loopback port the local API listens on. Zero, as in an
	// old settings file, is replaced by the default.
	AgentPort int `json:"agentPort,omitempty"`
	// 编码 agent 只保存本机命令与公开配置，登录凭据由 agent 自己管理。
	CodingAgents      []authoring.Profile          `json:"codingAgents,omitempty"`
	ActiveCodingAgent string                       `json:"activeCodingAgent,omitempty"`
	CodingConfig      map[string]map[string]string `json:"codingConfig,omitempty"`
	// DisablePresence turns off the system's confirmation before unlocking,
	// revealing a key or signing. The check is on by default where the
	// platform offers one.
	DisablePresence bool `json:"disablePresence,omitempty"`
	// SidebarCollapsed keeps the sidebar as an icon rail.
	SidebarCollapsed bool `json:"sidebarCollapsed,omitempty"`
	// HideFiat turns off the approximate USD values beside balances. Fiat is
	// on by default; the field is inverted so an old settings file, which has
	// no field, keeps it on.
	HideFiat bool             `json:"hideFiat,omitempty"`
	Accounts []wallet.Address `json:"accounts"`
	// Wallets is the native wallet: named containers, each holding at most
	// one account per chain family. Keys are in the OS keychain, never here.
	Wallets []wallet.Container `json:"wallets,omitempty"`
	// ActiveWallet is the container the Wallet page shows, or
	// wallet.PasskeyContainerID when the passkey identity is selected.
	ActiveWallet string `json:"activeWallet,omitempty"`
	// Wallet is the EVM accounts of Wallets, in container order. It is
	// rebuilt from Wallets on every load. An old file that has only this
	// field is migrated into Wallets.
	Wallet []wallet.Account `json:"wallet,omitempty"`
	// NearWallet is the NEAR accounts of Wallets. Their ed25519 keys are in
	// the keychain under near:key:<accountId>, never in this file.
	NearWallet []wallet.NearAccount `json:"nearWallet,omitempty"`
	// Passkey is the public half of the passkey the shell created: its
	// credential id, its P-256 public key, and the NEAR, EVM and Solana
	// accounts the key controls. The private key stays in the authenticator.
	Passkey *PasskeyAccount `json:"passkey,omitempty"`
}

const (
	// DefaultRefreshSeconds is how often balances are read again.
	DefaultRefreshSeconds = 60
	// MinRefreshSeconds keeps a public endpoint from being polled too hard.
	MinRefreshSeconds = 15
	// MaxRefreshSeconds is the slowest the settings offer.
	MaxRefreshSeconds = 3600
	// DefaultAgentPort is the local API's port until the user picks another.
	DefaultAgentPort = 8765
	// MinAgentPort excludes the privileged range, which the listener can
	// never bind without extra privileges.
	MinAgentPort = 1024
)

// DefaultSettings is the state before the user changes anything.
func DefaultSettings() Settings {
	return Settings{
		NetworkID:      chains.Default().ID,
		RefreshSeconds: DefaultRefreshSeconds,
		AgentPort:      DefaultAgentPort,
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
	if s.AgentPort < MinAgentPort || s.AgentPort > 65535 {
		s.AgentPort = DefaultAgentPort
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
	s.Wallets, s.ActiveWallet = normalizeWallets(s.Wallets, s.Wallet, s.NearWallet, s.ActiveWallet)
	s.Wallet, s.NearWallet = projectWallets(s.Wallets)
	seenWallet := make(map[string]bool, len(s.Wallet)+len(s.NearWallet))
	for _, a := range s.Wallet {
		seenWallet[a.Addr] = true
	}
	for _, a := range s.NearWallet {
		seenWallet[a.AccountID] = true
	}
	for _, w := range s.Wallets {
		if w.Solana != nil {
			seenWallet[w.Solana.Address] = true
		}
	}
	kept := accounts[:0]
	for _, a := range accounts {
		if seenWallet[a.Addr] {
			continue
		}
		kept = append(kept, a)
	}
	s.Accounts = kept
	s.Passkey = validatePasskey(s.Passkey)
	return s
}

// normalizeWallets turns an old flat wallet into containers, then drops
// accounts that are not valid or that repeat an address already kept.
// Containers are the source of truth after the first load. Legacy slices are
// migrated only when no container was stored, so a later edit of those slices
// cannot resurrect an account the user removed from a container.
func normalizeWallets(wallets []wallet.Container, legacyEVM []wallet.Account, legacyNear []wallet.NearAccount, active string) ([]wallet.Container, string) {
	// nil means an old settings file that has not been migrated. An empty
	// slice means the user has no wallets; do not rebuild them from the
	// projected fields, or deleting the last account would put it back.
	if wallets == nil {
		wallets = migrateLegacyWallets(legacyEVM, legacyNear)
	}
	seenEVM := map[string]bool{}
	seenNear := map[string]bool{}
	seenSol := map[string]bool{}
	seenID := map[string]bool{}
	out := make([]wallet.Container, 0, len(wallets))
	for _, w := range wallets {
		c := wallet.Container{Name: trimLabel(w.Name)}
		c.ID = cleanWalletID(w.ID)
		if c.ID == "" || c.ID == wallet.PasskeyContainerID || seenID[c.ID] {
			c.ID = fallbackWalletID(w, seenID)
		}
		seenID[c.ID] = true
		if w.EVM != nil {
			addr, err := wallet.Normalize(wallet.EVM, w.EVM.Addr)
			if err == nil && !seenEVM[addr] {
				seenEVM[addr] = true
				acc := wallet.Account{Addr: addr, Label: trimLabel(w.EVM.Label)}
				c.EVM = &acc
			}
		}
		if w.Near != nil {
			accountID, err := wallet.Normalize(wallet.Near, w.Near.AccountID)
			publicKey, err2 := neartx.ParsePublicKey(w.Near.PublicKey)
			if err == nil && err2 == nil && !seenNear[accountID] {
				seenNear[accountID] = true
				acc := wallet.NearAccount{
					AccountID: accountID,
					PublicKey: publicKey.String(),
					Label:     trimLabel(w.Near.Label),
				}
				c.Near = &acc
			}
		}
		if w.Solana != nil {
			addr, err := wallet.Normalize(wallet.Solana, w.Solana.Address)
			if err == nil && !seenSol[addr] {
				seenSol[addr] = true
				acc := wallet.SolanaAccount{Address: addr, Label: trimLabel(w.Solana.Label)}
				c.Solana = &acc
			}
		}
		if c.Name == "" {
			c.Name = fmt.Sprintf("Wallet %d", len(out)+1)
		}
		if !c.Empty() {
			out = append(out, c)
		}
	}
	if active == wallet.PasskeyContainerID {
		return out, active
	}
	for _, w := range out {
		if w.ID == active {
			return out, active
		}
	}
	if len(out) == 0 {
		return out, ""
	}
	return out, out[0].ID
}

// migrateLegacyWallets gives each old single-chain account its own container.
// They were not created as one identity, so they are not paired together.
func migrateLegacyWallets(legacyEVM []wallet.Account, legacyNear []wallet.NearAccount) []wallet.Container {
	out := make([]wallet.Container, 0, len(legacyEVM)+len(legacyNear))
	n := 1
	for _, acc := range legacyEVM {
		addr, err := wallet.Normalize(wallet.EVM, acc.Addr)
		if err != nil {
			continue
		}
		name := trimLabel(acc.Label)
		if name == "" {
			name = fmt.Sprintf("Wallet %d", n)
		}
		n++
		kept := wallet.Account{Addr: addr, Label: trimLabel(acc.Label)}
		out = append(out, wallet.Container{
			ID:   "legacy-evm-" + strings.TrimPrefix(addr, "0x"),
			Name: name,
			EVM:  &kept,
		})
	}
	for _, acc := range legacyNear {
		accountID, err := wallet.Normalize(wallet.Near, acc.AccountID)
		if err != nil {
			continue
		}
		if _, err := neartx.ParsePublicKey(acc.PublicKey); err != nil {
			continue
		}
		name := trimLabel(acc.Label)
		if name == "" {
			name = fmt.Sprintf("Wallet %d", n)
		}
		n++
		kept := acc
		kept.AccountID = accountID
		kept.Label = trimLabel(acc.Label)
		out = append(out, wallet.Container{
			ID:   "legacy-near-" + accountID,
			Name: name,
			Near: &kept,
		})
	}
	return out
}

func projectWallets(wallets []wallet.Container) ([]wallet.Account, []wallet.NearAccount) {
	var evm []wallet.Account
	var near []wallet.NearAccount
	for _, w := range wallets {
		if w.EVM != nil {
			evm = append(evm, *w.EVM)
		}
		if w.Near != nil {
			near = append(near, *w.Near)
		}
	}
	return evm, near
}

func cleanWalletID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 80 {
		return ""
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return ""
		}
	}
	return id
}

func fallbackWalletID(w wallet.Container, seen map[string]bool) string {
	raw := w.Name
	if w.EVM != nil {
		raw += w.EVM.Addr
	}
	if w.Near != nil {
		raw += w.Near.AccountID
	}
	if w.Solana != nil {
		raw += w.Solana.Address
	}
	sum := wallet.Keccak256([]byte(raw))
	id := "w-" + hexPrefix(sum)
	for seen[id] {
		raw += "\x00"
		sum = wallet.Keccak256([]byte(raw))
		id = "w-" + hexPrefix(sum)
	}
	return id
}

func hexPrefix(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 0; i < 8 && i < len(b); i++ {
		out[i*2] = hexdigits[b[i]>>4]
		out[i*2+1] = hexdigits[b[i]&0x0f]
	}
	return string(out)
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

// NetworkFor picks the preset a family is read on: the selected network
// when it belongs to the family, otherwise that family's mainnet. An
// unselected family never falls back to a testnet. A family with no
// mainnet preset is skipped.
func (s Settings) NetworkFor(family chain.Family) chain.Network {
	if sel := s.Network(); sel.Family == family {
		return sel
	}
	for _, n := range chains.All() {
		if n.Family == family && !testnetNetwork(n) {
			return n
		}
	}
	return chain.Network{}
}

// testnetNetwork reports a preset that must not be priced into the fiat
// total, and must not be the stand-in for a family the user did not select.
func testnetNetwork(n chain.Network) bool {
	return strings.EqualFold(n.ChainName, "testnet") || strings.Contains(strings.ToLower(n.ID), "testnet")
}

// ReaderFor builds the reader for a network. The custom endpoint belongs to
// the network it was set for; every other preset reads its own default.
func (s Settings) ReaderFor(network chain.Network) (chain.Reader, error) {
	rpc := ""
	if network.ID == s.NetworkID {
		rpc = s.RPCURL
	}
	return chains.NewReader(network, rpc)
}

// Has reports whether addr is already watched, by the watch list or the
// wallet.
func (s Settings) Has(addr string) bool {
	for _, a := range s.Accounts {
		if a.Addr == addr {
			return true
		}
	}
	return s.IsWalletAccount(addr)
}

// IsWallet reports whether addr is an EVM account of a wallet container.
func (s Settings) IsWallet(addr string) bool {
	for _, a := range s.Wallet {
		if a.Addr == addr {
			return true
		}
	}
	return false
}

// IsSolanaWallet reports whether addr is a Solana account of a wallet container.
func (s Settings) IsSolanaWallet(addr string) bool {
	for _, w := range s.Wallets {
		if w.Solana != nil && w.Solana.Address == addr {
			return true
		}
	}
	return false
}

// SolanaAccounts lists the Solana accounts of every wallet, in container order.
func (s Settings) SolanaAccounts() []wallet.SolanaAccount {
	var out []wallet.SolanaAccount
	for _, w := range s.Wallets {
		if w.Solana != nil {
			out = append(out, *w.Solana)
		}
	}
	return out
}

// IsNearWallet reports whether accountID is a NEAR account of the wallet.
func (s Settings) IsNearWallet(accountID string) bool {
	for _, a := range s.NearWallet {
		if a.AccountID == accountID {
			return true
		}
	}
	return false
}

// IsWalletAccount reports whether addr is an account of a wallet container.
func (s Settings) IsWalletAccount(addr string) bool {
	return s.IsWallet(addr) || s.IsNearWallet(addr) || s.IsSolanaWallet(addr)
}

// WatchedOf lists every address of one family the app reads: the watch list
// and the wallet's own accounts of that family.
func (s Settings) WatchedOf(family chain.Family) []wallet.Address {
	out := s.AccountsOf(family)
	seen := make(map[string]bool, len(out)+len(s.Wallets))
	for _, a := range out {
		seen[a.Addr] = true
	}
	add := func(addr, label string) {
		if addr == "" || seen[addr] {
			return
		}
		seen[addr] = true
		out = append(out, wallet.Address{Addr: addr, Label: label})
	}
	switch family {
	case chain.EVM:
		for _, a := range s.Wallet {
			add(a.Addr, a.Label)
		}
	case chain.Near:
		for _, a := range s.NearWallet {
			add(a.AccountID, a.Label)
		}
	case chain.Solana:
		for _, a := range s.SolanaAccounts() {
			add(a.Address, a.Label)
		}
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
	case wallet.Near:
		return chain.Near, true
	case wallet.Solana:
		return chain.Solana, true
	}
	return "", false
}
