package plugin

import (
	"errors"
	"path/filepath"
	"sync"
)

// 仅记录当前持有的 lease；释放即删除，避免历史 id 导致锁表增长。
var pluginLeases = struct {
	sync.Mutex
	active map[string]bool
}{active: map[string]bool{}}

const maxActivePluginLeases = 128

func validatePluginID(id string) error {
	if !idPattern.MatchString(id) || len(id) > 128 {
		return managementError("INVALID_PLUGIN_ID", "应用 id 无效", 400)
	}
	return nil
}

func installedError(err error) error {
	if errors.Is(err, ErrNotInstalled) {
		return managementError("PLUGIN_NOT_FOUND", "应用未安装", 404)
	}
	return managementError("BROKEN_PLUGIN", "无法读取已安装应用", 409)
}

func (s *Store) tryLease(id string) (func(), error) {
	if err := validatePluginID(id); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(s.dir)
	if err != nil {
		return nil, managementError("STORE_UNAVAILABLE", "应用目录不可用", 503)
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	key := filepath.Join(root, id)
	pluginLeases.Lock()
	defer pluginLeases.Unlock()
	if pluginLeases.active[key] || len(pluginLeases.active) >= maxActivePluginLeases {
		return nil, managementError("APP_BUSY", "应用正忙，请稍后重试", 409)
	}
	pluginLeases.active[key] = true
	var once sync.Once
	return func() {
		once.Do(func() { pluginLeases.Lock(); delete(pluginLeases.active, key); pluginLeases.Unlock() })
	}, nil
}

// TryExecution 与更新、卸载及设置共用非阻塞的按应用独占 lease。
// 宿主须覆盖 runner 构造、顶层加载和 Page 调用，完成后调用 release。
func (m *Manager) TryExecution(id string) (release func(), err error) {
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, managementError("MANAGER_CLOSED", "应用生命周期服务已关闭", 503)
	}
	return m.store.tryLease(id)
}

// SetSettings 供原生界面与 HTTP/ACP 共用同一设置协调入口。
func (m *Manager) SetSettings(id string, values map[string]string) ([]SettingStatus, error) {
	return m.store.SetSettings(id, values)
}

// SetEnabled 与执行及安装提交串行，避免开关状态在更新时丢失。
func (m *Manager) SetEnabled(id string, enabled bool) error { return m.store.SetEnabled(id, enabled) }
