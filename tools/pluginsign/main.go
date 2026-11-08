// Command pluginsign signs plugins for the crypto-panel store: it packs a
// plugin folder into an archive, hashes it, signs the hash, and writes the
// registry entry that lists it.
//
//	pluginsign keygen -out publisher.key
//	pluginsign pack -key publisher.key -dir internal/plugin/testdata/examples/gas \
//	    -url https://example.com/gas-1.0.0.zip \
//	    -zip gas-1.0.0.zip -entry gas.json
//	pluginsign registry -name "My plugins" -out registry.json gas.json
//
// The private key never leaves the publisher's machine; the registry only
// carries the public key, the hash and the signature.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/davirain-su/crypto-panel/internal/plugin"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "pack":
		err = pack(os.Args[2:])
	case "registry":
		err = registry(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pluginsign:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  pluginsign keygen -out publisher.key
  pluginsign pack -key publisher.key -dir <plugin folder> -url <archive URL> -zip <out.zip> -entry <out.json>
  pluginsign registry -name <catalog name> -out <registry.json> <entry.json>...
`)
	os.Exit(2)
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "publisher.key", "where to write the private key")
	fs.Parse(args)
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(priv)
	if err := os.WriteFile(*out, []byte(encoded+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Printf("private key: %s (keep it secret)\n", *out)
	fmt.Printf("publisher:   %s\n", plugin.PublicKeyString(pub))
	return nil
}

func pack(args []string) error {
	fs := flag.NewFlagSet("pack", flag.ExitOnError)
	keyPath := fs.String("key", "", "the publisher's private key")
	dir := fs.String("dir", "", "the plugin folder")
	url := fs.String("url", "", "where the archive will be served from")
	zipPath := fs.String("zip", "", "where to write the archive")
	entryPath := fs.String("entry", "", "where to write the registry entry")
	fs.Parse(args)
	if *keyPath == "" || *dir == "" || *url == "" || *zipPath == "" || *entryPath == "" {
		return errors.New("pack needs -key, -dir, -url, -zip and -entry")
	}
	key, err := readKey(*keyPath)
	if err != nil {
		return err
	}
	m, err := plugin.LoadManifest(*dir)
	if err != nil {
		return err
	}
	if err := m.ValidateDir(*dir); err != nil {
		return err
	}
	digest, err := plugin.DigestDir(*dir)
	if err != nil {
		return err
	}
	archive, err := plugin.ZipDir(*dir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*zipPath, archive, 0o644); err != nil {
		return err
	}
	entry := plugin.RegistryEntry{
		ID:          m.ID,
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Author:      m.Author,
		URL:         *url,
		Hash:        digest,
		Signature:   plugin.SignDigest(key, digest),
		Publisher:   plugin.PublicKeyString(key.Public().(ed25519.PublicKey)),
	}
	if err := entry.Validate(); err != nil {
		return err
	}
	if err := writeJSON(*entryPath, entry); err != nil {
		return err
	}
	fmt.Printf("packed %s %s → %s (%s)\n", m.ID, m.Version, *zipPath, digest)
	return nil
}

func registry(args []string) error {
	fs := flag.NewFlagSet("registry", flag.ExitOnError)
	name := fs.String("name", "Plugins", "the catalog's name")
	out := fs.String("out", "registry.json", "where to write the catalog")
	fs.Parse(args)
	if fs.NArg() == 0 {
		return errors.New("registry needs at least one entry file")
	}
	reg := plugin.Registry{Version: 1, Name: *name}
	for _, path := range fs.Args() {
		var entry plugin.RegistryEntry
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &entry); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := entry.Validate(); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		reg.Plugins = append(reg.Plugins, entry)
	}
	if _, err := plugin.ParseRegistry(mustJSON(reg)); err != nil {
		return err
	}
	if err := writeJSON(*out, reg); err != nil {
		return err
	}
	fmt.Printf("wrote %s with %d plugins\n", *out, len(reg.Plugins))
	return nil
}

func readKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("the key file is not an ed25519 private key")
	}
	return ed25519.PrivateKey(raw), nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
