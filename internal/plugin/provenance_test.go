package plugin

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
	return []byte(fmt.Sprintf(`{"id":%q,"signed":%t,"official":%t,"via":%q,"packageHash":%q,"installDigest":%q}`, rec.ID, rec.Signed, rec.Official, rec.Via, rec.PackageHash, rec.InstallDigest)), nil
}

// scriptedSecrets is a credential store whose reads and writes can fail,
// hang, or be recorded. Missing keys are not-found, not a read error.
type scriptedSecrets struct {
	mu       sync.Mutex
	m        map[string][]byte
	order    []string
	getErr   map[string]error
	getDelay time.Duration
	getWaits int
	setErr   error
	failSet  map[string]error
	setDelay time.Duration
	delErr   error
	gets     int
}

func (s *scriptedSecrets) Get(key string) ([]byte, error) {
	s.mu.Lock()
	delay := s.getDelay
	s.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	if err := s.getErr[key]; err != nil {
		delete(s.getErr, key)
		return nil, err
	}
	v, ok := s.m[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), v...), nil
}

func (s *scriptedSecrets) Set(key string, value []byte) error {
	if s.setDelay > 0 {
		time.Sleep(s.setDelay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, key)
	if err := s.failSet[key]; err != nil {
		return err
	}
	if s.setErr != nil {
		return s.setErr
	}
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[key] = append([]byte(nil), value...)
	return nil
}

func (s *scriptedSecrets) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.delErr != nil && strings.HasPrefix(key, "provenance:") {
		return s.delErr
	}
	delete(s.m, key)
	return nil
}

func plantLoose(t *testing.T, root, id string) {
	t.Helper()
	app := filepath.Join(root, id)
	manifest := strings.Replace(personalManifest, "local.personal.gas", id, 1)
	writePlugin(t, app, manifest, personalSource)
	if err := os.WriteFile(filepath.Join(app, "install.json"), []byte("{\"installedVia\":\"folder\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInstallSucceedsWhenProvenanceCannotBeStored(t *testing.T) {
	sec := &scriptedSecrets{setErr: errors.New("keychain unavailable")}
	s := NewStore(t.TempDir(), sec)
	if err := s.EnsureOfficial(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "com.example.gas", "plugin.json")); err != nil {
		t.Fatal("官方示例因凭据服务不可用而没有装上")
	}
	id := "local.personal.nokey"
	src := t.TempDir()
	manifest := strings.Replace(personalManifest, "local.personal.gas", id, 1)
	writePlugin(t, src, manifest, personalSource)
	if _, err := s.Install(src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), id, "plugin.json")); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
}

func TestReadErrorDoesNotMigrateForgedInstall(t *testing.T) {
	sec := &scriptedSecrets{m: map[string][]byte{}}
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "plugins"), sec)
	id := "local.personal.signedread"
	plantLoose(t, s.Dir(), id)
	signed, err := jsonMarshalProvenance(hostProvenance{ID: id, Signed: true, Via: "store", PackageHash: "sha256:" + strings.Repeat("ab", 32), InstallDigest: "sha256:" + strings.Repeat("cd", 32)})
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.Set(ProvenanceAccount(id), signed); err != nil {
		t.Fatal(err)
	}
	if err := sec.Set(provenanceMigrationAccount, []byte("done")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), id, "install.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sec.getErr = map[string]error{ProvenanceAccount(id): errors.New("keychain read failed")}
	m, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
	rec, found, err := s.getProvenance(id)
	if err != nil || !found || !rec.Signed {
		t.Fatalf("读错误后记录被改写: found=%v signed=%v err=%v", found, rec.Signed, err)
	}
	requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
}

func TestMigrationDoesNotOverwriteExistingRecord(t *testing.T) {
	sec := &scriptedSecrets{m: map[string][]byte{}}
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "plugins"), sec)
	id := "local.personal.keepsigned"
	plantLoose(t, s.Dir(), id)
	signed, err := jsonMarshalProvenance(hostProvenance{ID: id, Signed: true, Via: "store", PackageHash: "sha256:" + strings.Repeat("11", 32), InstallDigest: "sha256:" + strings.Repeat("22", 32)})
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.Set(ProvenanceAccount(id), signed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), id, "install.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.migrateProvenance()
	rec, found, err := s.getProvenance(id)
	if err != nil || !found || !rec.Signed {
		t.Fatalf("迁移覆盖了已有记录: %+v found=%v err=%v", rec, found, err)
	}
}

func TestMigrationMarkerIsWrittenAfterRecords(t *testing.T) {
	sec := &scriptedSecrets{m: map[string][]byte{}}
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "plugins"), sec)
	id := "local.personal.order"
	plantLoose(t, s.Dir(), id)
	s.migrateProvenance()
	sec.mu.Lock()
	order := append([]string(nil), sec.order...)
	sec.mu.Unlock()
	startedAt, recordAt, doneAt := -1, -1, -1
	for i, key := range order {
		switch key {
		case provenanceMigrationStarted:
			startedAt = i
		case ProvenanceAccount(id):
			recordAt = i
		case provenanceMigrationAccount:
			doneAt = i
		}
	}
	if startedAt < 0 || recordAt < 0 || doneAt < 0 || !(startedAt < recordAt && recordAt < doneAt) {
		t.Fatalf("迁移写入顺序错误: %v", order)
	}
}

func TestMigrationDoesNotFinishWhenARecordWriteFails(t *testing.T) {
	id := "local.personal.partial"
	sec := &scriptedSecrets{m: map[string][]byte{}, setErr: errors.New("write failed")}
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "plugins"), sec)
	plantLoose(t, s.Dir(), id)
	s.migrateProvenance()
	if _, found, err := s.readProv(provenanceMigrationAccount); err != nil || found {
		t.Fatalf("记录没写上却标记迁移完成 found=%v err=%v", found, err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), id, "install.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sec.setErr = nil
	s.migrateProvenance()
	rec, found, err := s.getProvenance(id)
	if err != nil || found || rec.ID != "" {
		t.Fatalf("失败后的再次迁移相信了伪造的 install.json: %+v found=%v err=%v", rec, found, err)
	}
}

func TestProvenanceWriteTimeoutDoesNotAuthorize(t *testing.T) {
	id := "local.personal.timeout"
	sec := &scriptedSecrets{m: map[string][]byte{}, setDelay: 1500 * time.Millisecond, setErr: errors.New("write failed")}
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "plugins"), sec)
	plantLoose(t, s.Dir(), id)
	start := time.Now()
	m, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	if time.Since(start) > 1600*time.Millisecond {
		t.Fatalf("凭据写入超时没有在时限内返回: %s", time.Since(start))
	}
	requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
	forged, err := jsonMarshalProvenance(hostProvenance{ID: id, Via: "folder"})
	if err != nil {
		t.Fatal(err)
	}
	s.provenanceMem = map[string][]byte{ProvenanceAccount(id): forged}
	if _, found, err := s.readProv(ProvenanceAccount(id)); found || err == nil {
		t.Fatal("超时后的内存记录被当成已确认")
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), id, "install.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nextSec := &scriptedSecrets{m: map[string][]byte{}}
	next := NewStore(s.Dir(), nextSec)
	nextMgr, err := NewManager(next)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { nextMgr.Close() })
	requireCode(t, nextMgr.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
	if _, found, _ := next.getProvenance(id); found {
		t.Fatal("下一次启动从伪造的 install.json 写出了未签名记录")
	}
}

func TestProvenanceTimeoutIsPerStoreAndRetries(t *testing.T) {
	old := provenanceBackoff
	provenanceBackoff = 20 * time.Millisecond
	t.Cleanup(func() { provenanceBackoff = old })
	slow := &scriptedSecrets{getDelay: 3 * time.Second}
	fast := &scriptedSecrets{m: map[string][]byte{}}
	if err := fast.Set("provenance:ready", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	s1 := NewStore(t.TempDir(), slow)
	s2 := NewStore(t.TempDir(), fast)
	start := time.Now()
	if _, _, err := s1.readProv("provenance:slow"); !errors.Is(err, errProvTimeout) || time.Since(start) > 1600*time.Millisecond {
		t.Fatalf("慢读没有按时失败: %v %s", err, time.Since(start))
	}
	if b, found, err := s2.readProv("provenance:ready"); err != nil || !found || string(b) != "ok" {
		t.Fatalf("一次超时拖住了另一个存储: %q found=%v err=%v", b, found, err)
	}
	slow.mu.Lock()
	slow.getDelay = 0
	slow.mu.Unlock()
	if err := slow.Set("provenance:later", []byte("yes")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, found, err := s1.readProv("provenance:later"); err != nil || !found {
		t.Fatalf("退避结束后没有重试: found=%v err=%v", found, err)
	}
}

func TestRemoveReportsProvenanceDeleteFailure(t *testing.T) {
	sec := &scriptedSecrets{m: map[string][]byte{}}
	s := NewStore(t.TempDir(), sec)
	id := "local.personal.delerr"
	src := t.TempDir()
	manifest := strings.Replace(personalManifest, "local.personal.gas", id, 1)
	writePlugin(t, src, manifest, personalSource)
	if _, err := s.Install(src); err != nil {
		t.Fatal(err)
	}
	sec.delErr = errors.New("delete blocked")
	m, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	err = m.Remove(id)
	if err == nil || !strings.Contains(err.Error(), ProvenanceAccount(id)) {
		t.Fatalf("卸载没有报告来源记录删除失败: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(s.Dir(), id)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("报告删除失败时应用目录还在")
	}
}

func TestSentinelBlocksRemigration(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			sec := &scriptedSecrets{m: map[string][]byte{}}
			root := t.TempDir()
			s := NewStore(filepath.Join(root, "plugins"), sec)
			id := "local.personal.sentinel" + kind
			plantLoose(t, s.Dir(), id)
			path, err := s.migrationSentinelPath()
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file":
				if err := os.WriteFile(path, []byte("1\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(root, "other-sentinel")
				if err := os.WriteFile(target, []byte("1\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			m, err := NewManager(s)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { m.Close() })
			requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
			if _, found, _ := s.getProvenance(id); found {
				t.Fatal("哨兵还在时迁移相信了 install.json")
			}
		})
	}
}

func TestStartedMarkerBlocksRemigration(t *testing.T) {
	sec := &scriptedSecrets{m: map[string][]byte{}}
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "plugins"), sec)
	id := "local.personal.started"
	plantLoose(t, s.Dir(), id)
	if err := sec.Set(provenanceMigrationStarted, []byte("1")); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
	if _, found, _ := s.getProvenance(id); found {
		t.Fatal("已开始标记还在时迁移相信了 install.json")
	}
}

func TestFailedSentinelCreateDoesNotMigrate(t *testing.T) {
	sec := &scriptedSecrets{m: map[string][]byte{}}
	root := t.TempDir()
	plugins := filepath.Join(root, "plugins")
	if err := os.MkdirAll(plugins, 0o700); err != nil {
		t.Fatal(err)
	}
	s := NewStore(plugins, sec)
	id := "local.personal.nosentinel"
	plantLoose(t, s.Dir(), id)
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	m, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
	if _, found, _ := s.getProvenance(id); found {
		t.Fatal("哨兵创建失败后仍然迁移了 install.json")
	}
}

func TestStartedWriteFailureDoesNotMigrate(t *testing.T) {
	id := "local.personal.startfail"
	sec := &scriptedSecrets{m: map[string][]byte{}, failSet: map[string]error{provenanceMigrationStarted: errors.New("cannot start")}}
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "plugins"), sec)
	plantLoose(t, s.Dir(), id)
	m, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
	if _, found, _ := s.getProvenance(id); found {
		t.Fatal("开始标记没写上却迁移了 install.json")
	}
}

func TestDoneMarkerReadErrorDoesNotMigrate(t *testing.T) {
	id := "local.personal.doneerr"
	sec := &scriptedSecrets{m: map[string][]byte{}, getErr: map[string]error{provenanceMigrationAccount: errors.New("keychain read failed")}}
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "plugins"), sec)
	plantLoose(t, s.Dir(), id)
	if err := os.WriteFile(filepath.Join(s.Dir(), id, "install.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
	if _, found, err := s.getProvenance(id); err != nil || found {
		t.Fatalf("完成标记读错误后仍迁移了伪造记录: found=%v err=%v", found, err)
	}
}

func TestAppRecordReadErrorDoesNotOverwriteSigned(t *testing.T) {
	id := "local.personal.keep"
	sec := &scriptedSecrets{m: map[string][]byte{}}
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "plugins"), sec)
	plantLoose(t, s.Dir(), id)
	signed, err := jsonMarshalProvenance(hostProvenance{ID: id, Signed: true, Via: "store", PackageHash: "sha256:" + strings.Repeat("ab", 32), InstallDigest: "sha256:" + strings.Repeat("cd", 32)})
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.Set(ProvenanceAccount(id), signed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), id, "install.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sec.getErr = map[string]error{ProvenanceAccount(id): errors.New("keychain read failed")}
	m, err := NewManager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	requireCode(t, m.APIMayMutate(id), "API_MUTATION_FORBIDDEN")
	rec, found, err := s.getProvenance(id)
	if err != nil || !found || !rec.Signed {
		t.Fatalf("应用记录读错误后被改成未签名: %+v found=%v err=%v", rec, found, err)
	}
}

func TestUnconfirmedProvenanceDoesNotRollBackInstall(t *testing.T) {
	sec := &scriptedSecrets{m: map[string][]byte{}, setErr: errors.New("keychain down")}
	s, m := newTestManager(t, sec)
	done, err := m.Decide(proposeTest(t, m, personalPackage(t, "local.personal.unconfirmed", "1.0.0"), "install", "").RequestID, true)
	if err != nil || done.Status != "installed" {
		t.Fatalf("凭据写失败不应撤回安装: %+v %v", done, err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), done.PluginID, "plugin.json")); err != nil {
		t.Fatal(err)
	}
	requireCode(t, m.APIMayMutate(done.PluginID), "API_MUTATION_FORBIDDEN")
}

func TestSwappedInstallRecordStillRollsBackCommit(t *testing.T) {
	s, m := newTestManager(t, nil)
	first := installTest(t, m, personalPackage(t, "local.personal.swapcommit", "1.0.0"))
	pending := proposeTest(t, m, personalPackage(t, first.PluginID, "2.0.0"), "update", first.PackageHash)
	dest := filepath.Join(s.Dir(), first.PluginID)
	m.rename = func(from, to string) error {
		if err := os.Rename(from, to); err != nil {
			return err
		}
		if to == dest {
			return os.WriteFile(filepath.Join(to, "install.json"), []byte("{}\n"), 0o600)
		}
		return nil
	}
	done, err := m.Decide(pending.RequestID, true)
	if err == nil || done.Status == "installed" {
		t.Fatalf("调包的 install.json 仍被提交: %+v %v", done, err)
	}
	requireCode(t, m.APIMayMutate(first.PluginID), "API_MUTATION_FORBIDDEN")
}

func TestRecoveryRejectsSignedInstallRecord(t *testing.T) {
	s, m := newTestManager(t, nil)
	first := installTest(t, m, personalPackage(t, "local.personal.recoverysigned", "1.0.0"))
	hash, err := s.PackageHash(first.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("{\n  \"installedVia\": \"local-api\",\n  \"packageHash\": %q,\n  \"hash\": %q,\n  \"publisher\": \"ed25519:attacker\",\n  \"signature\": \"forged\"\n}\n", hash, "sha256:"+strings.Repeat("ab", 32))
	if err := os.WriteFile(filepath.Join(s.Dir(), first.PluginID, "install.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	delete(s.provenanceMem, ProvenanceAccount(first.PluginID))
	tx, err := os.MkdirTemp(m.transactionDir, "commit-")
	if err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(commitRecord{PluginID: first.PluginID, Operation: "update", PackageHash: hash, ExpectedHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tx, "record.json"), record, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewManager(s)
	if restarted != nil {
		t.Cleanup(func() { restarted.Close() })
	}
	rec, found, gerr := s.getProvenance(first.PluginID)
	if gerr == nil && found && !rec.Signed {
		t.Fatal("恢复把带签名的 install.json 记成了未签名")
	}
	if err == nil {
		requireCode(t, restarted.APIMayMutate(first.PluginID), "API_MUTATION_FORBIDDEN")
	}
}
