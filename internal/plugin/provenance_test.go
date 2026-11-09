package plugin

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSignedAppUpdateRejectsTamperedInstallRecord(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "nohash", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			s, m := newTestManager(t, nil)
			id := "local.personal." + kind
			dir := t.TempDir()
			writeVersion(t, dir, id, "1.0.0", personalSource)
			_, private, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			entry := signEntry(t, dir, "https://example.com/signed.zip", private)
			archive, err := ZipDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := s.Prepare(archive, entry)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := prepared.Install(); err != nil {
				t.Fatal(err)
			}
			hash, err := s.PackageHash(id)
			if err != nil {
				t.Fatal(err)
			}
			record := filepath.Join(s.Dir(), id, "install.json")
			switch kind {
			case "missing":
				if err := os.Remove(record); err != nil {
					t.Fatal(err)
				}
			case "empty":
				if err := os.WriteFile(record, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "nohash":
				if err := os.WriteFile(record, []byte("{\"installedVia\":\"local-api\"}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(record); err != nil {
					t.Fatal(err)
				}
				forged := filepath.Join(t.TempDir(), "forged.json")
				if err := os.WriteFile(forged, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(forged, record); err != nil {
					t.Fatal(err)
				}
			}
			next := personalPackage(t, id, "2.0.0")
			_, err = m.Propose(Source{Kind: "inline", Package: &next}, "update", id, hash, "local-api")
			requireCode(t, err, "SOURCE_NOT_ALLOWED")
		})
	}
}

func TestRememberInstallRejectsSwappedRecord(t *testing.T) {
	s, m := newTestManager(t, nil)
	done := installTest(t, m, personalPackage(t, "local.personal.swap", "1.0.0"))
	info, _, err := readInstallBytes(filepath.Join(s.Dir(), done.PluginID))
	if err != nil {
		t.Fatal(err)
	}
	before, ok, _ := s.getProvenance(done.PluginID)
	if !ok {
		t.Fatal("安装没有写下宿主记录")
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), done.PluginID, "install.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.rememberInstall(done.PluginID, info, filepath.Join(s.Dir(), done.PluginID)); err == nil {
		t.Fatal("接受了被调包的 install.json")
	}
	after, ok, _ := s.getProvenance(done.PluginID)
	if !ok || after.InstallDigest != before.InstallDigest {
		t.Fatal("调包改写了宿主记录")
	}
}

func TestRemoveDeletesProvenanceRecord(t *testing.T) {
	s, m := newTestManager(t, nil)
	done := installTest(t, m, personalPackage(t, "local.personal.forget", "1.0.0"))
	if _, ok, _ := s.getProvenance(done.PluginID); !ok {
		t.Fatal("安装没有写下宿主记录")
	}
	if err := m.Remove(done.PluginID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.getProvenance(done.PluginID); ok {
		t.Fatal("卸载留下了宿主记录")
	}
}

type lockedSecrets struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *lockedSecrets) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), v...), nil
}
func (s *lockedSecrets) Set(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[key] = append([]byte(nil), value...)
	return nil
}
func (s *lockedSecrets) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func TestConcurrentInstallsKeepEveryProvenanceRecord(t *testing.T) {
	sec := &lockedSecrets{m: map[string][]byte{}}
	dir := t.TempDir()
	plugins := filepath.Join(dir, "plugins")
	s1 := NewStore(plugins, sec)
	s2 := NewStore(plugins, sec)
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("local.personal.n%02d", i)
			src, err := os.MkdirTemp(dir, "src-")
			if err != nil {
				errs <- err
				return
			}
			manifest := strings.Replace(personalManifest, "local.personal.gas", id, 1)
			if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte(manifest), 0o644); err != nil {
				errs <- err
				return
			}
			if err := os.WriteFile(filepath.Join(src, "index.js"), []byte(personalSource), 0o644); err != nil {
				errs <- err
				return
			}
			st := s1
			if i%2 == 1 {
				st = s2
			}
			if _, err := st.Install(src); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	kept := 0
	for i := range n {
		id := fmt.Sprintf("local.personal.n%02d", i)
		if _, ok, _ := s1.getProvenance(id); ok {
			kept++
		}
	}
	if kept != n {
		t.Fatalf("并发安装只保留了 %d/%d 条宿主记录", kept, n)
	}
}

func TestBadProvenanceFileDoesNotAuthorizeOrStopManager(t *testing.T) {
	for _, kind := range []string{"empty", "corrupt", "symlink", "corrupt-account"} {
		t.Run(kind, func(t *testing.T) {
			sec := &memSecrets{m: map[string][]byte{}}
			root := t.TempDir()
			s := NewStore(filepath.Join(root, "plugins"), sec)
			id := "local.personal.badfile"
			app := filepath.Join(s.Dir(), id)
			manifest := strings.Replace(personalManifest, "local.personal.gas", id, 1)
			writePlugin(t, app, manifest, personalSource)
			record := filepath.Join(app, "install.json")
			body := []byte("{\"installedVia\":\"folder\"}\n")
			if err := os.WriteFile(record, body, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := sec.Set(provenanceMigrationAccount, []byte("done")); err != nil {
				t.Fatal(err)
			}
			hash, err := normalizedPackageHash(app)
			if err != nil {
				t.Fatal(err)
			}
			forged, err := jsonMarshalProvenance(hostProvenance{ID: id, Via: "folder", PackageHash: hash, InstallDigest: installDigest(body)})
			if err != nil {
				t.Fatal(err)
			}
			index := s.ProvenancePath()
			switch kind {
			case "empty":
				if err := os.WriteFile(index, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(index, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(root, "forged-index.json")
				if err := os.WriteFile(target, forged, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, index); err != nil {
					t.Fatal(err)
				}
			case "corrupt-account":
				if err := sec.Set(ProvenanceAccount(id), []byte("{")); err != nil {
					t.Fatal(err)
				}
			}
			m, err := NewManager(s)
			if err != nil {
				t.Fatalf("不可信索引阻止了启动: %v", err)
			}
			t.Cleanup(func() { m.Close() })
			requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
			if err := m.Remove(id); err != nil {
				t.Fatalf("原生卸载失败: %v", err)
			}
		})
	}
}

func jsonMarshalProvenance(rec hostProvenance) ([]byte, error) {
	return []byte(fmt.Sprintf(`{"id":%q,"via":%q,"packageHash":%q,"installDigest":%q}`, rec.ID, rec.Via, rec.PackageHash, rec.InstallDigest)), nil
}
