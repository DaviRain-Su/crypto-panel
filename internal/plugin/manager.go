package plugin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type installationTask struct {
	record Installation
	dir    string
}

// Manager 负责进程内待确认任务与个人应用提交；它从不调用批准回调。
// 同一个 Store 在宿主中应只创建一个 Manager，并共享给 UI、HTTP 与 ACP。
type Manager struct {
	store             *Store
	pendingDir        string
	transactionDir    string
	mu                sync.Mutex
	tasks             map[string]*installationTask
	closed            bool
	stop              chan struct{}
	stopped           chan struct{}
	commits           sync.WaitGroup
	rename            func(string, string) error
	candidates        sync.WaitGroup
	candidateSlots    chan struct{}
	removalGeneration uint64
	viaGeneration     [2]uint64
}

// NewManager 先恢复已确认提交，再清除上次进程未确认的快照。
func NewManager(store *Store) (*Manager, error) {
	if store == nil {
		return nil, managementError("INVALID_STORE", "应用存储不可用", 503)
	}
	root := filepath.Join(store.dir, ".lifecycle")
	for _, dir := range []string{store.dir, root, filepath.Join(root, "pending"), filepath.Join(root, "transactions")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, managementError("STORE_UNAVAILABLE", "无法创建应用生命周期目录", 503)
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return nil, managementError("STORE_UNAVAILABLE", "应用生命周期目录无效", 503)
		}
	}
	m := &Manager{store: store, pendingDir: filepath.Join(root, "pending"), transactionDir: filepath.Join(root, "transactions"), tasks: map[string]*installationTask{}, stop: make(chan struct{}), stopped: make(chan struct{}), rename: os.Rename, candidateSlots: make(chan struct{}, MaxPendingInstallations)}
	if err := m.recoverTransactions(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(m.pendingDir)
	if err != nil {
		return nil, managementError("STORE_UNAVAILABLE", "无法清理旧候选", 503)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(m.pendingDir, entry.Name())); err != nil {
			return nil, managementError("STORE_UNAVAILABLE", "无法清理旧候选", 503)
		}
	}
	go m.sweep()
	return m, nil
}

func (m *Manager) sweep() {
	defer close(m.stopped)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-ticker.C:
			m.mu.Lock()
			m.pruneLocked(now.UTC())
			m.mu.Unlock()
		}
	}
}

// Propose 冻结并校验候选，立即返回 pending，不等待任何用户批准。
func (m *Manager) Propose(source Source, operation, targetID, expectedHash, via string) (Installation, error) {
	if operation != "install" && operation != "update" {
		return Installation{}, managementError("INVALID_OPERATION", "安装操作无效", 400)
	}
	if via != "local-api" && via != "acp" {
		return Installation{}, managementError("INVALID_VIA", "提议来源无效", 400)
	}
	if operation == "update" {
		if targetID == "" {
			return Installation{}, managementError("TARGET_REQUIRED", "更新必须指定应用 id", 400)
		}
		if _, err := parseHash(expectedHash); err != nil {
			return Installation{}, managementError("EXPECTED_HASH_REQUIRED", "更新必须指定合法旧包摘要", 400)
		}
	} else if expectedHash != "" {
		return Installation{}, managementError("INVALID_EXPECTED_HASH", "新安装不能携带旧包摘要", 400)
	}
	removalGeneration, viaGeneration, err := m.beginCandidate(via)
	if err != nil {
		return Installation{}, err
	}
	defer m.endCandidate()
	c, result, err := m.prepareCandidate(source)
	if err != nil {
		return Installation{}, err
	}
	if !result.Valid {
		return Installation{}, managementError("VALIDATION_FAILED", "候选应用未通过静态校验", 422)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(c.dir)
		}
	}()
	if !strings.HasPrefix(c.manifest.ID, "local.") || len(strings.Split(c.manifest.ID, ".")) != 3 {
		return Installation{}, managementError("PERSONAL_NAMESPACE_REQUIRED", "个人应用必须使用 local.<owner>.<slug> 命名空间", 422)
	}
	if targetID != "" && targetID != c.manifest.ID {
		return Installation{}, managementError("PLUGIN_ID_MISMATCH", "目标 id 与候选清单不一致", 409)
	}
	// 生产路径为空。测试在任务写入前关闭入口，以观察代际是否作废仍在校验的提议。
	if beforeProposalCommit != nil {
		beforeProposalCommit()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Installation{}, managementError("MANAGER_CLOSED", "应用生命周期服务已关闭", 503)
	}
	if m.removalGeneration != removalGeneration || m.viaGeneration[viaIndex(via)] != viaGeneration {
		return Installation{}, managementError("PROPOSAL_CANCELLED", "提议期间来源已取消或应用被卸载，请重新提议", 409)
	}
	m.pruneLocked(time.Now().UTC())
	active := 0
	for _, task := range m.tasks {
		if !activeInstallation(task.record.Status) {
			continue
		}
		active++
		if task.record.PluginID != c.manifest.ID {
			continue
		}
		// 相同内容只复用同一入口的任务。API 不能挂到 ACP 任务上，否则关闭 API 撤不掉它。
		if task.record.Operation == operation && task.record.PackageHash == c.hash && task.record.ExpectedHash == expectedHash && task.record.Via == via {
			return cloneInstallation(task.record), nil
		}
		return Installation{}, managementError("CANDIDATE_CONFLICT", "该应用已有其他待处理候选", 409)
	}
	if active >= MaxPendingInstallations {
		return Installation{}, managementError("QUEUE_FULL", "待安装队列已满", 429)
	}
	release, err := m.store.tryLease(c.manifest.ID)
	if err != nil {
		return Installation{}, err
	}
	defer release()
	old, err := m.checkTarget(c.manifest, operation, expectedHash)
	if err != nil {
		return Installation{}, err
	}
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Installation{}, managementError("REQUEST_ID_UNAVAILABLE", "无法创建安装任务", 503)
	}
	now := time.Now().UTC()
	record := Installation{RequestID: hex.EncodeToString(random[:]), Operation: operation, PluginID: c.manifest.ID, Version: c.manifest.Version, PackageHash: c.hash, ExpectedHash: expectedHash, Status: "pending", Via: via, Manifest: c.manifest, CreatedAt: now, ExpiresAt: now.Add(InstallationTTL)}
	if old != nil {
		manifest := old.Manifest
		record.OldManifest = &manifest
	}
	m.tasks[record.RequestID] = &installationTask{record: record, dir: c.dir}
	keep = true
	return cloneInstallation(record), nil
}

func activeInstallation(status string) bool { return status == "pending" || status == "installing" }

// GetInstallation 返回防御性副本；读取时也会落实过期。
func (m *Manager) GetInstallation(id string) (Installation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now().UTC())
	task, ok := m.tasks[id]
	if !ok {
		return Installation{}, managementError("INSTALLATION_NOT_FOUND", "安装任务不存在", 404)
	}
	return cloneInstallation(task.record), nil
}

// Installations 仅列出尚未结束的任务，不把候选混入活跃应用列表。
func (m *Manager) Installations() []Installation {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now().UTC())
	out := []Installation{}
	for _, task := range m.tasks {
		if activeInstallation(task.record.Status) {
			out = append(out, cloneInstallation(task.record))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Decide 只供宿主原生事件调用。状态锁外提交，绝不执行任何批准回调。
func (m *Manager) Decide(id string, allow bool) (Installation, error) {
	m.mu.Lock()
	m.pruneLocked(time.Now().UTC())
	task, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return Installation{}, managementError("INSTALLATION_NOT_FOUND", "安装任务不存在", 404)
	}
	if m.closed || task.record.Status != "pending" {
		record := cloneInstallation(task.record)
		m.mu.Unlock()
		return record, managementError("INSTALLATION_NOT_PENDING", "安装任务已不在待确认状态", 409)
	}
	if !allow {
		m.finishLocked(task, "rejected", "", "")
		record := cloneInstallation(task.record)
		m.mu.Unlock()
		return record, nil
	}
	release, err := m.store.tryLease(task.record.PluginID)
	if err != nil {
		record := cloneInstallation(task.record)
		m.mu.Unlock()
		return record, err
	}
	task.record.Status = "installing"
	m.commits.Add(1)
	m.mu.Unlock()
	defer m.commits.Done()
	defer release()
	err = m.commit(task)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		var safe *Error
		if !errors.As(err, &safe) {
			safe = managementError("INSTALLATION_FAILED", "应用提交失败，旧版已保留", 500)
		}
		m.finishLocked(task, "failed", safe.Code, safe.Message)
		return cloneInstallation(task.record), safe
	}
	m.finishLocked(task, "installed", "", "")
	return cloneInstallation(task.record), nil
}

// beforeProposalCommit 仅测试设置。它在候选通过校验、写入任务表之前运行。
var beforeProposalCommit func()

// APIMayMutate 只放行未签名的 local.* 个人应用。官方和已签名应用仍由原生界面卸载和改设置。
// 尚未安装的 id 不在这里报错，因此删除一个已经不存在的 local.* 应用保持幂等。
func (m *Manager) APIMayMutate(id string) error {
	if !strings.HasPrefix(id, "local.") {
		return managementError("API_MUTATION_FORBIDDEN", "本机 API 只能卸载或修改未签名的 local.* 个人应用", 403)
	}
	inst, err := m.store.Get(id)
	if errors.Is(err, ErrNotInstalled) {
		return nil
	}
	if err != nil {
		return installedError(err)
	}
	if inst.Official || inst.Signed || !strings.HasPrefix(inst.Manifest.ID, "local.") {
		return managementError("API_MUTATION_FORBIDDEN", "本机 API 只能卸载或修改未签名的 local.* 个人应用", 403)
	}
	return nil
}

// Cancel 仅取消 pending，不能宣称撤销正在提交的安装。
func (m *Manager) Cancel(id string) (Installation, error) {
	return m.cancel(id, "")
}

// CancelFrom 只取消指定入口的任务。本机 API 不能取消 ACP 发起的安装。
func (m *Manager) CancelFrom(id, via string) (Installation, error) {
	if via != "local-api" && via != "acp" {
		return Installation{}, managementError("INVALID_VIA", "提议来源无效", 400)
	}
	return m.cancel(id, via)
}

func (m *Manager) cancel(id, via string) (Installation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now().UTC())
	task, ok := m.tasks[id]
	if !ok {
		return Installation{}, managementError("INSTALLATION_NOT_FOUND", "安装任务不存在", 404)
	}
	if via != "" && task.record.Via != via {
		return cloneInstallation(task.record), managementError("INSTALLATION_ORIGIN_FORBIDDEN", "本机 API 不能取消其他来源的安装任务", 403)
	}
	if task.record.Status == "cancelled" {
		return cloneInstallation(task.record), nil
	}
	if task.record.Status != "pending" {
		return cloneInstallation(task.record), managementError("INSTALLATION_NOT_PENDING", "安装任务无法取消", 409)
	}
	m.finishLocked(task, "cancelled", "", "")
	return cloneInstallation(task.record), nil
}

// CancelVia 使已关闭入口的未批准任务失效；已经开始提交的任务继续完成。
func (m *Manager) CancelVia(via string) {
	if via != "local-api" && via != "acp" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.viaGeneration[viaIndex(via)]++
	m.pruneLocked(time.Now().UTC())
	for _, task := range m.tasks {
		if task.record.Via == via && task.record.Status == "pending" {
			m.finishLocked(task, "cancelled", "", "")
		}
	}
}

// Remove 与执行、设置、提交共用 lease，并先撤销同 id 的待确认任务。
func (m *Manager) Remove(id string) error {
	release, err := m.store.tryLease(id)
	if err != nil {
		return err
	}
	defer release()
	m.mu.Lock()
	m.removalGeneration++
	for _, task := range m.tasks {
		if task.record.PluginID == id && task.record.Status == "pending" {
			m.finishLocked(task, "cancelled", "", "")
		}
	}
	m.mu.Unlock()
	if err := m.recoverBeforeRemove(id); err != nil {
		return err
	}
	return m.store.remove(id)
}

// Close 取消全部 pending，等待已获准的提交完成；不会恢复批准。
func (m *Manager) Close() error {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		close(m.stop)
		for _, task := range m.tasks {
			if task.record.Status == "pending" {
				m.finishLocked(task, "cancelled", "", "")
			}
		}
	}
	m.mu.Unlock()
	<-m.stopped
	m.candidates.Wait()
	m.commits.Wait()
	return nil
}

func viaIndex(via string) int {
	if via == "acp" {
		return 1
	}
	return 0
}

func (m *Manager) beginCandidate(via string) (uint64, uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, 0, managementError("MANAGER_CLOSED", "应用生命周期服务已关闭", 503)
	}
	select {
	case m.candidateSlots <- struct{}{}:
		m.candidates.Add(1)
		return m.removalGeneration, m.viaGeneration[viaIndex(via)], nil
	default:
		return 0, 0, managementError("QUEUE_FULL", "候选校验并发数量已满，请稍后重试", 429)
	}
}
func (m *Manager) endCandidate() { <-m.candidateSlots; m.candidates.Done() }

func (m *Manager) finishLocked(task *installationTask, status, code, message string) {
	task.record.Status = status
	task.record.Code = code
	task.record.Error = message
	task.record.CompletedAt = time.Now().UTC()
	if task.dir != "" {
		_ = os.RemoveAll(task.dir)
		task.dir = ""
	}
	m.trimHistoryLocked(time.Now().UTC())
}

func (m *Manager) pruneLocked(now time.Time) {
	for _, task := range m.tasks {
		if task.record.Status == "pending" && !now.Before(task.record.ExpiresAt) {
			m.finishLocked(task, "expired", "INSTALLATION_EXPIRED", "待确认任务已过期")
		}
	}
	m.trimHistoryLocked(now)
}

func (m *Manager) trimHistoryLocked(now time.Time) {
	type terminal struct {
		id string
		at time.Time
	}
	var history []terminal
	for id, task := range m.tasks {
		if activeInstallation(task.record.Status) {
			continue
		}
		if !now.Before(task.record.CompletedAt.Add(InstallationHistoryTTL)) {
			delete(m.tasks, id)
			continue
		}
		history = append(history, terminal{id: id, at: task.record.CompletedAt})
	}
	sort.Slice(history, func(i, j int) bool { return history[i].at.Before(history[j].at) })
	for i := 0; i < len(history)-MaxInstallationHistory; i++ {
		delete(m.tasks, history[i].id)
	}
}

func cloneManifest(m Manifest) Manifest {
	m.Permissions.Network = append([]string(nil), m.Permissions.Network...)
	m.Permissions.Chains = append([]string(nil), m.Permissions.Chains...)
	m.Permissions.Read = append([]string(nil), m.Permissions.Read...)
	m.Settings = append([]SettingField(nil), m.Settings...)
	m.Navigation = append([]NavItem(nil), m.Navigation...)
	return m
}
func cloneInstallation(record Installation) Installation {
	record.Manifest = cloneManifest(record.Manifest)
	if record.OldManifest != nil {
		old := cloneManifest(*record.OldManifest)
		record.OldManifest = &old
	}
	return record
}
