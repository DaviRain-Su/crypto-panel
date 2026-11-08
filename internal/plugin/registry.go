package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/davirain-su/crypto-panel/internal/version"
)

// A registry is the plugin store's catalog: a static JSON file that lists
// signed plugin archives. The app ships one (the featured plugins) and the
// user may point it at another. The registry is not trusted with code: every
// archive is checked against the hash and signature the entry names before
// it is unpacked into the plugins folder.

// Limits on what a registry and one download may bring.
const (
	// MaxRegistryBytes bounds the catalog file.
	MaxRegistryBytes = 1 << 20
	// MaxArchiveBytes bounds one plugin download.
	MaxArchiveBytes    = 8 << 20
	maxRegistryPlugins = 200
)

// Registry is one catalog file.
type Registry struct {
	Version int             `json:"version"`
	Name    string          `json:"name,omitempty"`
	Plugins []RegistryEntry `json:"plugins"`
}

// RegistryEntry is one plugin in a catalog. URL is where the archive lives;
// Hash is the digest of the unpacked tree, Signature the publisher's
// ed25519 signature over it, and Publisher the public key that made it.
type RegistryEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	URL         string `json:"url"`
	Hash        string `json:"hash"`
	Signature   string `json:"signature"`
	Publisher   string `json:"publisher"`
}

// ParseRegistry decodes and validates a catalog. Unknown fields are ignored,
// so a newer registry may add metadata this app does not read; the version
// field gates a format this app cannot understand.
func ParseRegistry(data []byte) (Registry, error) {
	if len(data) > MaxRegistryBytes {
		return Registry{}, fmt.Errorf("the registry is larger than %d bytes", MaxRegistryBytes)
	}
	var reg Registry
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&reg); err != nil {
		return Registry{}, fmt.Errorf("registry: %w", err)
	}
	if dec.More() {
		return Registry{}, errors.New("registry: trailing data after the catalog")
	}
	if reg.Version != 1 {
		return Registry{}, fmt.Errorf("registry version %d is not supported", reg.Version)
	}
	if len([]rune(reg.Name)) > 80 {
		return Registry{}, errors.New("registry: the name is longer than 80 characters")
	}
	if len(reg.Plugins) > maxRegistryPlugins {
		return Registry{}, fmt.Errorf("registry: more than %d plugins", maxRegistryPlugins)
	}
	seen := make(map[string]bool, len(reg.Plugins))
	for i := range reg.Plugins {
		entry := &reg.Plugins[i]
		if err := entry.Validate(); err != nil {
			return Registry{}, fmt.Errorf("registry entry %d: %w", i+1, err)
		}
		if seen[entry.ID] {
			return Registry{}, fmt.Errorf("registry: %s is listed twice", entry.ID)
		}
		seen[entry.ID] = true
	}
	return reg, nil
}

// Validate checks one entry's fields. The archive itself is checked when it
// is downloaded.
func (e RegistryEntry) Validate() error {
	if !idPattern.MatchString(e.ID) || len(e.ID) > 128 {
		return fmt.Errorf("id %q must be reverse-DNS, lowercase, such as \"com.example.portfolio\"", e.ID)
	}
	if strings.TrimSpace(e.Name) == "" || len([]rune(e.Name)) > 64 {
		return fmt.Errorf("%s: the name must be 1 to 64 characters", e.ID)
	}
	if !versionRegex.MatchString(e.Version) {
		return fmt.Errorf("%s: version %q is not semver (x.y.z)", e.ID, e.Version)
	}
	if len([]rune(e.Description)) > 400 {
		return fmt.Errorf("%s: the description is longer than 400 characters", e.ID)
	}
	if len([]rune(e.Author)) > 80 {
		return fmt.Errorf("%s: the author is longer than 80 characters", e.ID)
	}
	if !ValidEntryURL(e.URL) {
		return fmt.Errorf("%s: url %q must be https, or embedded: for the built-in catalog", e.ID, e.URL)
	}
	if _, err := parseHash(e.Hash); err != nil {
		return fmt.Errorf("%s: %w", e.ID, err)
	}
	if _, err := parseSignature(e.Signature); err != nil {
		return fmt.Errorf("%s: %w", e.ID, err)
	}
	if _, err := parsePublicKey(e.Publisher); err != nil {
		return fmt.Errorf("%s: %w", e.ID, err)
	}
	return nil
}

// ValidRegistryURL reports a URL a catalog may be fetched from: https
// anywhere, or http on the loopback interface for local development.
func ValidRegistryURL(raw string) bool { return validHTTPURL(raw) }

// ValidEntryURL reports a URL a plugin archive may be fetched from, which
// includes the app's own embedded catalog.
func ValidEntryURL(raw string) bool {
	if name, ok := strings.CutPrefix(raw, "embedded:"); ok {
		return name != "" && name == path.Base(name) && strings.HasSuffix(name, ".zip")
	}
	return validHTTPURL(raw)
}

func validHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}
	return false
}

// DefaultClient is the HTTP client for the store: a bounded wait, and
// redirects that stay inside the same URL policy.
func DefaultClient() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if !ValidRegistryURL(req.URL.String()) {
				return fmt.Errorf("redirect to %s is not allowed", req.URL.Redacted())
			}
			return nil
		},
	}
}

// FetchRegistry downloads and parses a catalog.
func FetchRegistry(ctx context.Context, client *http.Client, rawURL string) (Registry, error) {
	if !ValidRegistryURL(rawURL) {
		return Registry{}, fmt.Errorf("%q is not an https registry URL", rawURL)
	}
	data, err := fetch(ctx, client, rawURL, MaxRegistryBytes)
	if err != nil {
		return Registry{}, err
	}
	return ParseRegistry(data)
}

// FetchArchive downloads one plugin archive.
func FetchArchive(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	if !ValidRegistryURL(rawURL) {
		return nil, fmt.Errorf("%q is not an https plugin URL", rawURL)
	}
	return fetch(ctx, client, rawURL, MaxArchiveBytes)
}

func fetch(ctx context.Context, client *http.Client, rawURL string, max int64) ([]byte, error) {
	if client == nil {
		client = DefaultClient()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "crypto-panel/"+version.Version)
	req.Header.Set("Accept", "application/json, application/zip, */*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", rawURL, resp.Status)
	}
	return readLimited(resp.Body, max)
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("the download is larger than %d bytes", max)
	}
	return data, nil
}

// DigestDir is the canonical digest of a plugin folder: "sha256:" over its
// files, sorted by path, each written as path, NUL, size, NUL, contents. The
// shell's own files (state.json, kv.json, install.json) and the junk a file
// manager leaves behind are not part of it, so enabling a plugin or opening
// its folder does not change what was signed.
func DigestDir(dir string) (string, error) {
	files, err := pluginFiles(dir)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(data))
		h.Write(data)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// SignDigest signs a digest with the publisher's key, in the form a registry
// entry carries.
func SignDigest(key ed25519.PrivateKey, digest string) string {
	return "ed25519:" + base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(digest)))
}

// PublicKeyString renders a publisher key in the form a registry carries.
func PublicKeyString(key ed25519.PublicKey) string {
	return "ed25519:" + base64.StdEncoding.EncodeToString(key)
}

// VerifyDigest checks that signature over digest was made by publisher.
func VerifyDigest(digest, signature, publisher string) error {
	pub, err := parsePublicKey(publisher)
	if err != nil {
		return err
	}
	sig, err := parseSignature(signature)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, []byte(digest), sig) {
		return errors.New("the plugin's signature does not match its contents")
	}
	return nil
}

func parseHash(s string) (string, error) {
	raw, ok := strings.CutPrefix(s, "sha256:")
	if !ok || len(raw) != 64 {
		return "", errors.New(`hash must be "sha256:" and 64 hex characters`)
	}
	if strings.ToLower(raw) != raw {
		return "", errors.New("hash must be lowercase hex")
	}
	if _, err := hex.DecodeString(raw); err != nil {
		return "", errors.New("hash is not hex")
	}
	return s, nil
}

func parseSignature(s string) ([]byte, error) {
	raw, ok := strings.CutPrefix(s, "ed25519:")
	if !ok {
		return nil, errors.New(`signature must be "ed25519:" and base64`)
	}
	sig, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("signature is not an ed25519 signature")
	}
	return sig, nil
}

func parsePublicKey(s string) (ed25519.PublicKey, error) {
	raw, ok := strings.CutPrefix(s, "ed25519:")
	if !ok {
		return nil, errors.New(`publisher must be "ed25519:" and base64`)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("publisher is not an ed25519 public key")
	}
	return ed25519.PublicKey(key), nil
}

// ZipDir packs a plugin folder the way the store expects: the files the
// digest covers, and nothing else.
func ZipDir(dir string) ([]byte, error) {
	files, err := pluginFiles(dir)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		header := &zip.FileHeader{
			Name:     rel,
			Method:   zip.Deflate,
			Modified: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		header.SetMode(0o644)
		w, err := zw.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pluginFiles lists the files of a plugin folder that make up its identity,
// in sorted order, enforcing the same limits an install does.
func pluginFiles(dir string) ([]string, error) {
	var files []string
	total := int64(0)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "__MACOSX" {
				return fs.SkipDir
			}
			return nil
		}
		if isJunk(rel) || isShellFile(rel) {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("plugin contains a symlink: %s", rel)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("plugin contains a special file: %s", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxPluginFile {
			return fmt.Errorf("%s is larger than %d bytes", rel, maxPluginFile)
		}
		files = append(files, rel)
		total += info.Size()
		if len(files) > maxPluginFiles {
			return fmt.Errorf("plugin has more than %d files", maxPluginFiles)
		}
		if total > maxPluginTotal {
			return fmt.Errorf("plugin is larger than %d bytes", maxPluginTotal)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// isShellFile reports the files the shell owns inside a plugin folder; they
// are never part of a plugin's digest or of an archive.
func isShellFile(rel string) bool {
	switch rel {
	case "state.json", "kv.json", "install.json":
		return true
	}
	return false
}

// isJunk reports files a file manager or an archive tool leaves behind.
func isJunk(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == "__MACOSX" {
			return true
		}
	}
	switch path.Base(rel) {
	case ".DS_Store", "Thumbs.db":
		return true
	}
	return false
}

// CompareVersions orders two x.y.z versions, a prerelease before its
// release. It returns -1, 0 or 1.
func CompareVersions(a, b string) int { return compareVersions(a, b) }
