package plugin

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

func (m *Manager) checkTarget(manifest Manifest, operation, expectedHash string) (*Installed, error) {
	dest := filepath.Join(m.store.dir, manifest.ID)
	_, statErr := os.Lstat(dest)
	if operation == "install" {
		if statErr == nil {
			return nil, managementError("PLUGIN_EXISTS", "应用 id 已安装，请明确提议更新", 409)
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return nil, managementError("STORE_UNAVAILABLE", "无法读取应用目录", 503)
		}
		return nil, nil
	}
	inst, err := m.store.Get(manifest.ID)
	if err != nil {
		return nil, managementError("BASE_CHANGED", "已安装内容发生变化，请重新读取后提议更新", 409)
	}
	if err := m.store.authorizeUpdate(inst); err != nil {
		return nil, err
	}
	hash, err := m.store.PackageHash(manifest.ID)
	if err != nil || hash != expectedHash {
		return nil, managementError("BASE_CHANGED", "已安装内容发生变化，请重新读取后提议更新", 409)
	}
	if CompareVersions(manifest.Version, inst.Manifest.Version) <= 0 {
		return nil, managementError("VERSION_CONFLICT", "新版本必须高于已安装版本", 409)
	}
	if err := m.checkMigration(inst, manifest); err != nil {
		return nil, err
	}
	return &inst, nil
}

func (m *Manager) checkMigration(old Installed, next Manifest) error {
	st, err := loadState(old.Dir)
	if err != nil {
		return managementError("BASE_CHANGED", "已安装应用状态损坏", 409)
	}
	for _, field := range old.Manifest.Settings {
		newField, ok := next.setting(field.Key)
		if ok && newField.Secret == field.Secret {
			continue
		}
		if ok {
			return managementError("SETTINGS_MIGRATION_REQUIRED", "更新不能改变现有设置的 secret 属性", 409)
		}
		value := st.Settings[field.Key]
		if field.Secret {
			if m.store.secrets == nil {
				return managementError("CREDENTIALS_UNAVAILABLE", "无法检查旧凭据，更新未提交", 503)
			}
			secret, err := m.store.secrets.Get(secretKey(old.Manifest.ID, field.Key))
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return managementError("CREDENTIALS_UNAVAILABLE", "无法检查旧凭据，更新未提交", 503)
			}
			value = string(secret)
		}
		if value != "" {
			return managementError("SETTINGS_MIGRATION_REQUIRED", "更新移除或改变了有值设置，请先清空相应字段", 409)
		}
	}
	kv := &KV{path: filepath.Join(old.Dir, "kv.json")}
	lock := kv.fileLock()
	lock.Lock()
	defer lock.Unlock()
	if err := kv.load(); err != nil {
		return managementError("BASE_CHANGED", "应用存储数据损坏，更新未提交", 409)
	}
	if kv.size > next.Permissions.StorageKb*1024 {
		return managementError("STORAGE_QUOTA_CONFLICT", "新存储额度低于当前数据占用", 409)
	}
	return nil
}

type commitRecord struct {
	PluginID     string `json:"pluginId"`
	Operation    string `json:"operation"`
	PackageHash  string `json:"packageHash"`
	ExpectedHash string `json:"expectedHash,omitempty"`
}

func (m *Manager) commit(task *installationTask) error {
	record := task.record
	if err := checkFrozen(task.dir, record.PackageHash, record.Manifest); err != nil {
		return err
	}
	old, err := m.checkTarget(record.Manifest, record.Operation, record.ExpectedHash)
	if err != nil {
		return err
	}
	tx, err := os.MkdirTemp(m.transactionDir, "commit-")
	if err != nil {
		return managementError("INSTALLATION_FAILED", "无法准备安装事务，旧版已保留", 500)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tx)
		}
	}()
	next := filepath.Join(tx, "new")
	backup := filepath.Join(tx, "old")
	dest := filepath.Join(m.store.dir, record.PluginID)
	if m.rename(task.dir, next) != nil {
		return managementError("INSTALLATION_FAILED", "无法冻结安装事务，旧版已保留", 500)
	}
	st := state{Enabled: true}
	if old != nil {
		st, err = loadState(old.Dir)
		if err != nil {
			return managementError("BASE_CHANGED", "应用状态发生变化，旧版已保留", 409)
		}
		filtered := map[string]string{}
		for _, field := range record.Manifest.Settings {
			if !field.Secret {
				filtered[field.Key] = st.Settings[field.Key]
			}
		}
		st.Settings = filtered
	}
	// 复用已有 KV 文件锁，覆盖最终数据复制与目录切换，不修改 KV 读写实现。
	kv := &KV{path: filepath.Join(dest, "kv.json")}
	kvLock := kv.fileLock()
	kvLock.Lock()
	defer kvLock.Unlock()
	if old != nil {
		if err := kv.load(); err != nil {
			return managementError("BASE_CHANGED", "应用存储数据损坏，旧版已保留", 409)
		}
		if kv.size > record.Manifest.Permissions.StorageKb*1024 {
			return managementError("STORAGE_QUOTA_CONFLICT", "新存储额度低于当前数据占用", 409)
		}
		data, err := os.ReadFile(kv.path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return managementError("INSTALLATION_FAILED", "无法复制应用存储，旧版已保留", 500)
		}
		if err == nil {
			if os.WriteFile(filepath.Join(next, "kv.json"), data, 0600) != nil {
				return managementError("INSTALLATION_FAILED", "无法复制应用存储，旧版已保留", 500)
			}
		}
	}
	if saveState(next, st) != nil {
		return managementError("INSTALLATION_FAILED", "无法保存应用状态，旧版已保留", 500)
	}
	info := installInfo{InstalledVia: record.Via, PackageHash: record.PackageHash, Installed: time.Now().UTC().Format(time.RFC3339)}
	if saveInstallInfo(next, info) != nil {
		return managementError("INSTALLATION_FAILED", "无法保存安装来源，旧版已保留", 500)
	}
	if syncTree(next) != nil {
		return managementError("INSTALLATION_FAILED", "无法持久化候选，旧版已保留", 500)
	}
	journal := commitRecord{PluginID: record.PluginID, Operation: record.Operation, PackageHash: record.PackageHash, ExpectedHash: record.ExpectedHash}
	data, _ := json.Marshal(journal)
	if writeDurable(filepath.Join(tx, "record.json"), data) != nil || syncDirectory(m.transactionDir) != nil {
		return managementError("INSTALLATION_FAILED", "无法持久化提交记录，旧版已保留", 500)
	}
	rollback := func() error {
		if old != nil {
			if err := m.rename(backup, dest); err != nil {
				cleanup = false
				return managementError("RECOVERY_REQUIRED", "提交中断，旧版已保留等待启动恢复", 500)
			}
			if syncDirectory(m.store.dir) != nil {
				cleanup = false
				return managementError("RECOVERY_REQUIRED", "旧版已恢复，目录等待重新持久化", 500)
			}
		}
		return managementError("INSTALLATION_FAILED", "应用切换失败，旧版已保留", 500)
	}
	if old != nil {
		if m.rename(dest, backup) != nil {
			return managementError("INSTALLATION_FAILED", "无法准备旧版备份，旧版已保留", 500)
		}
		if syncDirectory(m.store.dir) != nil || syncDirectory(tx) != nil {
			return rollback()
		}
	}
	if err := m.rename(next, dest); err != nil {
		return rollback()
	}
	// 目录持久化失败也尝试回退；不能把一次失败报告成静默成功的新版本。
	if syncDirectory(m.store.dir) != nil || syncDirectory(tx) != nil {
		if m.rename(dest, next) != nil {
			cleanup = false
			return managementError("RECOVERY_REQUIRED", "目录切换需要启动恢复核对，旧版备份已保留", 500)
		}
		return rollback()
	}
	if err := m.store.rememberInstall(record.PluginID, info, dest); err != nil {
		if old == nil {
			_ = os.RemoveAll(dest)
			return managementError("INSTALLATION_FAILED", "无法记录安装来源，新版本已撤回", 500)
		}
		return rollback()
	}
	return nil
}

func checkFrozen(dir, hash string, manifest Manifest) error {
	for _, name := range []string{"state.json", "kv.json", "install.json"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, fs.ErrNotExist) {
			return managementError("CANDIDATE_CHANGED", "候选快照已改变，请重新提议", 409)
		}
	}
	files, err := pluginFiles(dir)
	if err != nil || len(files) == 0 {
		return managementError("CANDIDATE_CHANGED", "候选快照已改变，请重新提议", 409)
	}
	got, err := DigestDir(dir)
	if err != nil || got != hash {
		return managementError("CANDIDATE_CHANGED", "候选快照已改变，请重新提议", 409)
	}
	actual, err := LoadManifest(dir)
	if err != nil {
		return managementError("CANDIDATE_CHANGED", "候选清单已改变，请重新提议", 409)
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(manifest)
	if string(a) != string(b) || actual.ValidateDir(dir) != nil {
		return managementError("CANDIDATE_CHANGED", "候选清单已改变，请重新提议", 409)
	}
	return nil
}

func writeDurable(file string, data []byte) error {
	if err := writeFileAtomic(file, data, 0600); err != nil {
		return err
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return syncDirectory(filepath.Dir(file))
}
func syncDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func syncTree(dir string) error {
	return filepath.WalkDir(dir, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return syncDirectory(file)
		}
		if !entry.Type().IsRegular() {
			return errors.New("invalid file")
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		err = f.Sync()
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

// 调用方已持有 id 的 lease；卸载前消解残留事务，防止下一次启动恢复旧应用。
func (m *Manager) recoverBeforeRemove(id string) error {
	entries, err := os.ReadDir(m.transactionDir)
	if err != nil {
		return managementError("RECOVERY_REQUIRED", "无法核对安装事务，卸载未完成", 503)
	}
	for _, entry := range entries {
		tx := filepath.Join(m.transactionDir, entry.Name())
		data, err := readSourceFile(filepath.Join(tx, "record.json"), 4096)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		// 尚未写入记录的其他候选不会恢复为活跃应用。
		var safe *Error
		if errors.As(err, &safe) && safe.Code == "SOURCE_NOT_FOUND" {
			continue
		}
		var record commitRecord
		if err != nil || DecodeJSON(data, &record) != nil {
			return managementError("RECOVERY_REQUIRED", "安装事务记录无效，卸载未完成", 503)
		}
		if record.PluginID == id {
			if err := m.recoverTransaction(tx, record); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) recoverTransactions() error {
	entries, err := os.ReadDir(m.transactionDir)
	if err != nil {
		return managementError("RECOVERY_FAILED", "无法读取安装恢复记录", 503)
	}
	for _, entry := range entries {
		tx := filepath.Join(m.transactionDir, entry.Name())
		info, err := os.Lstat(tx)
		if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return managementError("RECOVERY_FAILED", "安装恢复目录无效", 503)
		}
		data, err := readSourceFile(filepath.Join(tx, "record.json"), 4096)
		var readErr *Error
		if errors.As(err, &readErr) && readErr.Code == "SOURCE_NOT_FOUND" {
			if _, oldErr := os.Lstat(filepath.Join(tx, "old")); !errors.Is(oldErr, fs.ErrNotExist) {
				return managementError("RECOVERY_FAILED", "安装备份缺少恢复记录", 503)
			}
			if os.RemoveAll(tx) != nil {
				return managementError("RECOVERY_FAILED", "无法清理未开始的安装事务", 503)
			}
			continue
		}
		var record commitRecord
		if err != nil || len(data) > 4096 || DecodeJSON(data, &record) != nil || validatePluginID(record.PluginID) != nil {
			return managementError("RECOVERY_FAILED", "安装恢复记录无效，旧版备份已保留", 503)
		}
		if _, err := parseHash(record.PackageHash); err != nil {
			return managementError("RECOVERY_FAILED", "安装恢复摘要无效", 503)
		}
		if record.Operation != "install" && record.Operation != "update" {
			return managementError("RECOVERY_FAILED", "安装恢复操作无效", 503)
		}
		if record.Operation == "update" {
			if _, err := parseHash(record.ExpectedHash); err != nil {
				return managementError("RECOVERY_FAILED", "旧版恢复摘要无效", 503)
			}
		} else if record.ExpectedHash != "" {
			return managementError("RECOVERY_FAILED", "安装恢复基线无效", 503)
		}
		release, err := m.store.tryLease(record.PluginID)
		if err != nil {
			return err
		}
		err = m.recoverTransaction(tx, record)
		release()
		if err != nil {
			return err
		}
	}
	return nil
}

func validateRecoveryState(dir string, requireState bool) error {
	data, err := readSourceFile(filepath.Join(dir, "state.json"), MaxSettingsBytes)
	var missing *Error
	if !(errors.As(err, &missing) && missing.Code == "SOURCE_NOT_FOUND" && !requireState) {
		var st state
		if err != nil || DecodeJSON(data, &st) != nil {
			return errors.New("invalid state")
		}
	}
	kv := &KV{path: filepath.Join(dir, "kv.json")}
	if err := kv.load(); err != nil {
		return errors.New("invalid storage")
	}
	return nil
}

func (m *Manager) recoverTransaction(tx string, record commitRecord) error {
	dest := filepath.Join(m.store.dir, record.PluginID)
	backup := filepath.Join(tx, "old")
	_, oldErr := os.Lstat(backup)
	if oldErr != nil && !errors.Is(oldErr, fs.ErrNotExist) {
		return managementError("RECOVERY_FAILED", "无法读取旧版备份", 503)
	}
	hash, hashErr := m.store.PackageHash(record.PluginID)
	info, infoErr := loadInstallInfo(dest)
	stErr := validateRecoveryState(dest, true)
	if hashErr == nil && infoErr == nil && stErr == nil && hash == record.PackageHash && info.PackageHash == record.PackageHash && (info.InstalledVia == "local-api" || info.InstalledVia == "acp") {
		if err := m.store.adoptPersonalRecord(record.PluginID, dest, record.PackageHash); err != nil {
			return managementError("RECOVERY_FAILED", "无法记录已恢复应用的安装来源", 503)
		}
		if syncDirectory(m.store.dir) != nil || os.RemoveAll(tx) != nil {
			return managementError("RECOVERY_FAILED", "无法完成新版提交恢复", 503)
		}
		return nil
	}
	if oldErr == nil {
		info, err := os.Lstat(backup)
		if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return managementError("RECOVERY_FAILED", "旧版备份无效", 503)
		}
		oldHash, err := normalizedPackageHash(backup)
		if err != nil || oldHash != record.ExpectedHash || validateRecoveryState(backup, false) != nil {
			return managementError("RECOVERY_FAILED", "旧版备份内容或状态损坏，事务已保留", 503)
		}
		if _, err := os.Lstat(dest); err == nil {
			if os.Rename(dest, filepath.Join(tx, "discarded")) != nil {
				return managementError("RECOVERY_FAILED", "无法移开未完成的新版本", 503)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return managementError("RECOVERY_FAILED", "无法读取活跃应用目录", 503)
		}
		if os.Rename(backup, dest) != nil || syncDirectory(m.store.dir) != nil {
			return managementError("RECOVERY_FAILED", "无法恢复旧版，备份已保留", 503)
		}
	} else if record.Operation == "update" {
		if hashErr != nil || hash != record.ExpectedHash {
			return managementError("RECOVERY_FAILED", "旧版或恢复备份缺失，事务已保留", 503)
		}
	} else {
		if _, err := os.Lstat(dest); err == nil {
			return managementError("RECOVERY_FAILED", "新安装目标出现其他内容，事务已保留", 503)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return managementError("RECOVERY_FAILED", "无法读取安装目标", 503)
		}
	}
	if os.RemoveAll(tx) != nil {
		return managementError("RECOVERY_FAILED", "无法清理安装恢复记录", 503)
	}
	return nil
}
