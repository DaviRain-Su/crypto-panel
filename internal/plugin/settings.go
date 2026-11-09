package plugin

import (
	"errors"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/egoist/mygo"
)

// DescribeSettings 返回完整声明元数据，秘密字段仅返回配置状态。
func (s *Store) DescribeSettings(id string) ([]SettingStatus, error) {
	release, err := s.tryLease(id)
	if err != nil {
		return nil, err
	}
	defer release()
	return s.describeSettings(id)
}

func (s *Store) describeSettings(id string) ([]SettingStatus, error) {
	inst, err := s.Get(id)
	if err != nil {
		return nil, installedError(err)
	}
	if inst.Err != "" {
		return nil, managementError("BROKEN_PLUGIN", "应用内容或来源记录已损坏", 409)
	}
	st, err := loadState(inst.Dir)
	if err != nil {
		return nil, managementError("SETTINGS_UNAVAILABLE", "无法读取应用设置", 500)
	}
	statuses := make([]SettingStatus, 0, len(inst.Manifest.Settings))
	for _, field := range inst.Manifest.Settings {
		status := SettingStatus{SettingField: field}
		if field.Secret {
			configured := false
			if s.secrets != nil {
				value, err := s.secrets.Get(secretKey(id, field.Key))
				configured = err == nil && len(value) > 0
			}
			status.Configured = &configured
		} else {
			value := st.Settings[field.Key]
			status.Value = &value
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// SetSettings 在任何写入前验证全部字段，平台失败仅报告键名。
func (s *Store) SetSettings(id string, values map[string]string) ([]SettingStatus, error) {
	release, err := s.tryLease(id)
	if err != nil {
		return nil, err
	}
	defer release()
	inst, err := s.Get(id)
	if err != nil {
		return nil, installedError(err)
	}
	if inst.Err != "" {
		return nil, managementError("BROKEN_PLUGIN", "应用内容或来源记录已损坏", 409)
	}
	keys := make([]string, 0, len(values))
	total := 0
	for key, value := range values {
		if _, ok := inst.Manifest.setting(key); !ok {
			return nil, managementError("UNDECLARED_SETTING", "请求包含未声明的设置键", 422)
		}
		if len(value) > MaxSettingValueBytes {
			return nil, managementError("SETTING_TOO_LARGE", "设置值超过大小上限", 413)
		}
		total += len(key) + len(value)
		if total > MaxSettingsBytes {
			return nil, managementError("SETTINGS_TOO_LARGE", "设置批次超过大小上限", 413)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	st, err := loadState(inst.Dir)
	if err != nil {
		return nil, managementError("SETTINGS_UNAVAILABLE", "无法读取应用设置", 500)
	}
	if st.Settings == nil {
		st.Settings = map[string]string{}
	}
	plain := []string{}
	applied := []string{}
	failed := []string{}
	for _, key := range keys {
		field, _ := inst.Manifest.setting(key)
		if !field.Secret {
			st.Settings[key] = values[key]
			plain = append(plain, key)
		}
	}
	if len(plain) > 0 {
		if saveState(inst.Dir, st) != nil {
			failed = append(failed, plain...)
		} else {
			applied = append(applied, plain...)
		}
	}
	for _, key := range keys {
		field, _ := inst.Manifest.setting(key)
		if !field.Secret {
			continue
		}
		var writeErr error
		if s.secrets == nil {
			writeErr = errors.New("credential store unavailable")
		} else if values[key] == "" {
			writeErr = s.secrets.Delete(secretKey(id, key))
			// 删除缺失的 key 不是错误：目标状态（key 不存在）已达成。
			if errors.Is(writeErr, fs.ErrNotExist) || errors.Is(writeErr, mygo.ErrSecretNotFound) {
				writeErr = nil
			}
		} else {
			writeErr = s.secrets.Set(secretKey(id, key), []byte(values[key]))
		}
		if writeErr != nil {
			failed = append(failed, key)
		} else {
			applied = append(applied, key)
		}
	}
	sort.Strings(applied)
	sort.Strings(failed)
	if len(failed) > 0 {
		return nil, &SettingsWriteError{AppliedKeys: applied, FailedKeys: failed}
	}
	return s.describeSettings(id)
}

func (s *Store) setEnabled(id string, enabled bool) error {
	inst, err := s.Get(id)
	if err != nil {
		return installedError(err)
	}
	st, err := loadState(inst.Dir)
	if err != nil {
		return managementError("SETTINGS_UNAVAILABLE", "无法读取应用状态", 500)
	}
	st.Enabled = enabled
	if saveState(filepath.Join(s.dir, id), st) != nil {
		return managementError("SETTINGS_WRITE_FAILED", "无法保存应用状态", 500)
	}
	return nil
}
