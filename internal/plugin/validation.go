package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MoeForge/moejs/syntax"
	"github.com/davirain-su/lintel/internal/chains"
)

type candidate struct {
	dir      string
	manifest Manifest
	hash     string
}

type candidateError struct{ issue ValidationError }

func (e *candidateError) Error() string { return e.issue.Message }
func invalidCandidate(field, code, message string) error {
	return &candidateError{issue: ValidationError{Path: field, Code: code, Message: message}}
}

// Validate 只读入并解析候选，不执行任何应用代码或宿主能力。
func (m *Manager) Validate(source Source) (ValidationResult, error) {
	if _, _, err := m.beginCandidate(""); err != nil {
		return ValidationResult{Errors: []ValidationError{}}, err
	}
	defer m.endCandidate()
	c, result, err := m.prepareCandidate(source)
	if c != nil {
		_ = os.RemoveAll(c.dir)
	}
	return result, err
}

func (m *Manager) prepareCandidate(source Source) (*candidate, ValidationResult, error) {
	result := ValidationResult{Errors: []ValidationError{}}
	if err := validateSource(source); err != nil {
		return nil, result, err
	}
	dir, err := os.MkdirTemp(m.pendingDir, "candidate-")
	if err != nil {
		return nil, result, managementError("SOURCE_UNAVAILABLE", "无法创建候选快照", 503)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	if source.Kind == "folder" {
		err = copyCandidate(source.Path, dir)
	} else {
		var pkg Package
		if source.Kind == "inline" {
			pkg = *source.Package
			pkg.Plugin = append(json.RawMessage(nil), pkg.Plugin...)
			if len(pkg.Plugin)+len(pkg.Main) > MaxPackageBytes {
				err = tooLarge()
			}
		} else {
			var data []byte
			data, err = readSourceFile(source.Path, MaxPackageBytes)
			if err == nil && DecodeJSON(data, &pkg) != nil {
				err = invalidCandidate("package", "INVALID_PACKAGE", "应用包必须是仅含 plugin 和 main 的严格 JSON 对象")
			}
		}
		if err == nil && len(pkg.Plugin) > maxManifestBytes {
			err = tooLarge()
		}
		if err == nil && len(pkg.Main) > maxSourceBytes {
			err = tooLarge()
		}
		if err == nil {
			var manifest Manifest
			if DecodeJSON(pkg.Plugin, &manifest) != nil {
				err = invalidCandidate("plugin", "INVALID_MANIFEST", "清单 JSON 字段、类型或重复键无效")
			} else if manifest.Main != "index.js" {
				err = invalidCandidate("plugin.main", "INVALID_MAIN", "单文件包的 main 必须为 index.js")
			}
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, "plugin.json"), pkg.Plugin, 0600)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, "index.js"), []byte(pkg.Main), 0600)
		}
	}
	if err != nil {
		var invalid *candidateError
		if errors.As(err, &invalid) {
			result.Errors = append(result.Errors, invalid.issue)
			return nil, result, nil
		}
		var safe *Error
		if errors.As(err, &safe) {
			return nil, result, safe
		}
		return nil, result, sourceIOError(err)
	}
	manifest, issues, err := normalizeCandidate(dir)
	if err != nil {
		return nil, result, err
	}
	if len(issues) > 0 {
		result.Errors = issues
		return nil, result, nil
	}
	hash, err := DigestDir(dir)
	if err != nil {
		return nil, result, managementError("VALIDATION_FAILED", "无法计算候选摘要", 422)
	}
	keep = true
	result.Valid = true
	result.PackageHash = hash
	result.Summary = &manifest
	return &candidate{dir: dir, manifest: manifest, hash: hash}, result, nil
}

func validateSource(source Source) error {
	switch source.Kind {
	case "inline":
		if source.Package == nil || source.Path != "" {
			return managementError("INVALID_SOURCE", "inline 来源只能指定 package", 400)
		}
	case "folder", "file":
		if source.Package != nil || source.Path == "" || !filepath.IsAbs(source.Path) || strings.ContainsAny(source.Path, "\x00\r\n") || strings.Contains(source.Path, "://") {
			return managementError("INVALID_SOURCE", "文件来源必须仅指定本机绝对路径", 400)
		}
	default:
		return managementError("INVALID_SOURCE", "不支持的应用来源", 400)
	}
	return nil
}

func sourceIOError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return managementError("SOURCE_NOT_FOUND", "应用来源不存在", 404)
	}
	return managementError("SOURCE_UNREADABLE", "无法读取应用来源", 422)
}
func tooLarge() error {
	return managementError("PACKAGE_TOO_LARGE", "应用来源超过大小或文件数量上限", 413)
}

func readSourceFile(file string, limit int64) ([]byte, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return nil, sourceIOError(err)
	}
	if !info.Mode().IsRegular() {
		return nil, invalidCandidate("source", "INVALID_FILE", "来源必须为普通文件，不能为链接或特殊文件")
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, sourceIOError(err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, invalidCandidate("source", "SOURCE_CHANGED", "读取时来源发生变化")
	}
	if opened.Size() > limit {
		return nil, tooLarge()
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, sourceIOError(err)
	}
	if int64(len(data)) > limit {
		return nil, tooLarge()
	}
	return data, nil
}

// copyCandidate 对每个输入文件只读取一次；Root 将并发路径替换限制在源树中。
// 排除的壳文件同样先检查类型，不能用同名链接绕过文件树检查。
func copyCandidate(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return sourceIOError(err)
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return invalidCandidate("source", "INVALID_TREE", "来源必须为普通目录，不能为链接")
	}
	root, err := os.OpenRoot(src)
	if err != nil {
		return sourceIOError(err)
	}
	defer root.Close()
	count, total := 0, int64(0)
	return fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return sourceIOError(walkErr)
		}
		if rel == "." {
			return nil
		}
		if strings.ContainsAny(rel, "\\\x00") || path.Clean(rel) != rel || path.IsAbs(rel) {
			return invalidCandidate("files", "INVALID_PATH", "应用文件路径无效")
		}
		stat, err := root.Lstat(rel)
		if err != nil {
			return sourceIOError(err)
		}
		if stat.Mode()&fs.ModeSymlink != 0 || (!stat.IsDir() && !stat.Mode().IsRegular()) {
			return invalidCandidate("files", "INVALID_FILE", "应用文件树包含链接或特殊文件")
		}
		if stat.IsDir() {
			if isJunk(rel) || isShellFile(rel) {
				return fs.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, filepath.FromSlash(rel)), 0700)
		}
		if isJunk(rel) || isShellFile(rel) {
			return nil
		}
		count++
		if count > maxPluginFiles || stat.Size() > maxPluginFile {
			return tooLarge()
		}
		f, err := root.Open(rel)
		if err != nil {
			return sourceIOError(err)
		}
		opened, err := f.Stat()
		if err != nil || !os.SameFile(stat, opened) {
			f.Close()
			return invalidCandidate("files", "SOURCE_CHANGED", "读取时应用文件发生变化")
		}
		data, err := io.ReadAll(io.LimitReader(f, maxPluginFile+1))
		_ = f.Close()
		if err != nil {
			return sourceIOError(err)
		}
		total += int64(len(data))
		if len(data) > maxPluginFile || total > maxPluginTotal {
			return tooLarge()
		}
		return os.WriteFile(filepath.Join(dst, filepath.FromSlash(rel)), data, 0600)
	})
}

func normalizeCandidate(dir string) (Manifest, []ValidationError, error) {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return Manifest{}, []ValidationError{{Path: "plugin", Code: "MISSING_MANIFEST", Message: "缺少可读取的清单"}}, nil
	}
	if len(data) > maxManifestBytes {
		return Manifest{}, nil, tooLarge()
	}
	var m Manifest
	if DecodeJSON(data, &m) != nil {
		return Manifest{}, []ValidationError{{Path: "plugin", Code: "INVALID_MANIFEST", Message: "清单 JSON 字段、类型或重复键无效"}}, nil
	}
	issues := manifestIssues(m)
	if len(issues) > 0 {
		return Manifest{}, issues, nil
	}
	if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(m.Main))); err == nil && info.Size() > maxSourceBytes {
		return Manifest{}, nil, tooLarge()
	}
	if err := m.ValidateDir(dir); err != nil {
		return Manifest{}, []ValidationError{{Path: "plugin.main", Code: "INVALID_MAIN", Message: "主模块缺失或类型无效"}}, nil
	}
	files, err := pluginFiles(dir)
	if err != nil {
		return Manifest{}, []ValidationError{{Path: "files", Code: "INVALID_TREE", Message: "应用文件树无效"}}, nil
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, rel := range files {
		if path.Ext(rel) != ".js" && path.Ext(rel) != ".mjs" {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return Manifest{}, nil, managementError("VALIDATION_FAILED", "无法读取候选模块", 422)
		}
		if rel == m.Main && len(source) > maxSourceBytes {
			return Manifest{}, nil, tooLarge()
		}
		if !utf8.Valid(source) {
			issues = append(issues, ValidationError{Path: "main", Code: "INVALID_SYNTAX", Message: "模块必须为 UTF-8 JavaScript"})
			continue
		}
		_, err = syntax.ParseModule(rel, string(source), syntax.Options{Stop: func() error {
			if time.Now().After(deadline) {
				return errors.New("parse timeout")
			}
			return nil
		}})
		if err != nil {
			issues = append(issues, ValidationError{Path: "main", Code: "INVALID_SYNTAX", Message: "JavaScript 模块语法无效或解析超过时限"})
		}
	}
	if len(issues) > 0 {
		return Manifest{}, issues, nil
	}
	canonical, err := json.Marshal(m)
	if err != nil {
		return Manifest{}, nil, managementError("VALIDATION_FAILED", "无法规范化清单", 422)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), append(canonical, '\n'), 0600); err != nil {
		return Manifest{}, nil, managementError("VALIDATION_FAILED", "无法写入候选快照", 503)
	}
	return m, nil, nil
}

func manifestIssues(m Manifest) []ValidationError {
	issues := []ValidationError{}
	add := func(field, code, message string) {
		issues = append(issues, ValidationError{Path: field, Code: code, Message: message})
	}
	if !idPattern.MatchString(m.ID) || len(m.ID) > 128 {
		add("plugin.id", "INVALID_ID", "应用 id 格式无效")
	}
	if strings.TrimSpace(m.Name) == "" || len([]rune(m.Name)) > 64 || !safeMetadata(m.Name) {
		add("plugin.name", "INVALID_NAME", "名称必须为 1 到 64 个安全字符")
	}
	if !versionRegex.MatchString(m.Version) {
		add("plugin.version", "INVALID_VERSION", "版本必须为 x.y.z 形式")
	}
	if err := validateMain(m.Main); err != nil || isJunk(m.Main) || isShellFile(m.Main) {
		add("plugin.main", "INVALID_MAIN", "主模块必须为应用目录内的合法 JS 相对路径")
	}
	for i, id := range m.Permissions.Chains {
		if _, ok := chains.ByID(id); !ok {
			add(fmt.Sprintf("plugin.permissions.chains[%d]", i), "UNKNOWN_CHAIN", "不支持该链")
		}
	}
	for i, host := range m.Permissions.Network {
		if !hostPattern.MatchString(strings.ToLower(host)) {
			add(fmt.Sprintf("plugin.permissions.network[%d]", i), "INVALID_HOST", "网络权限必须为合法主机名")
		}
	}
	for i, read := range m.Permissions.Read {
		if !readable[read] {
			add(fmt.Sprintf("plugin.permissions.read[%d]", i), "UNKNOWN_READ_SCOPE", "不支持该读取范围")
		}
	}
	if !safeMetadata(m.Description) || !safeMetadata(m.Author) || len([]rune(m.Author)) > 80 {
		add("plugin.description", "INVALID_TEXT", "清单文本包含不安全字符或超过长度限制")
	}
	seen := map[string]bool{}
	for i, field := range m.Settings {
		if seen[field.Key] {
			add(fmt.Sprintf("plugin.settings[%d].key", i), "DUPLICATE_SETTING", "设置 key 不可重复")
		}
		seen[field.Key] = true
		if validateSetting(field) != nil || !safeMetadata(field.Label) || !safeMetadata(field.Help) {
			add(fmt.Sprintf("plugin.settings[%d]", i), "INVALID_SETTING", "设置字段元数据无效")
		}
	}
	seen = map[string]bool{}
	for i, nav := range m.Navigation {
		if seen[nav.Page] {
			add(fmt.Sprintf("plugin.navigation[%d].page", i), "DUPLICATE_PAGE", "导航页面不可重复")
		}
		seen[nav.Page] = true
		if !safeMetadata(nav.Title) || !safeMetadata(nav.Icon) {
			add(fmt.Sprintf("plugin.navigation[%d]", i), "INVALID_NAVIGATION", "导航文本包含不安全字符")
		}
	}
	if len(issues) == 0 && m.validate() != nil {
		add("plugin", "INVALID_MANIFEST", "清单字段或权限不符合限制")
	}
	return issues
}

func safeMetadata(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) || r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return false
		}
	}
	return utf8.ValidString(value)
}

// PackageHash 读取安装目录的规范化内容身份，同时保留既有来源完整性检查。
func (s *Store) PackageHash(id string) (string, error) {
	if err := validatePluginID(id); err != nil {
		return "", err
	}
	inst, err := s.Get(id)
	if err != nil {
		return "", installedError(err)
	}
	if inst.Err != "" {
		return "", managementError("BROKEN_PLUGIN", "应用内容或来源记录已损坏", 409)
	}
	return normalizedPackageHash(inst.Dir)
}

func normalizedPackageHash(dir string) (string, error) {
	parent, err := os.MkdirTemp("", "lintel-hash-")
	if err != nil {
		return "", managementError("HASH_UNAVAILABLE", "无法计算应用摘要", 503)
	}
	defer os.RemoveAll(parent)
	if err := copyCandidate(dir, parent); err != nil {
		return "", managementError("BROKEN_PLUGIN", "应用文件树无效", 409)
	}
	manifest, err := LoadManifest(parent)
	if err != nil || manifest.ValidateDir(parent) != nil {
		return "", managementError("BROKEN_PLUGIN", "应用清单或主模块无效", 409)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return "", managementError("BROKEN_PLUGIN", "应用清单无效", 409)
	}
	if err := os.WriteFile(filepath.Join(parent, "plugin.json"), append(data, '\n'), 0600); err != nil {
		return "", managementError("HASH_UNAVAILABLE", "无法计算应用摘要", 503)
	}
	hash, err := DigestDir(parent)
	if err != nil {
		return "", managementError("BROKEN_PLUGIN", "应用文件树无效", 409)
	}
	return hash, nil
}

// OfficialPackage 只从随壳内嵌的两个模板读取完整内容。
func OfficialPackage(id string) (Package, string, error) {
	if !isOfficialID(id) {
		return Package{}, "", managementError("EXAMPLE_NOT_FOUND", "官方模板不存在", 404)
	}
	if err := prepareOfficial(); err != nil {
		return Package{}, "", managementError("EXAMPLE_UNAVAILABLE", "无法读取官方模板", 503)
	}
	root := officialRoots[id]
	data, err := fs.ReadFile(officialFS, path.Join(root, "plugin.json"))
	if err != nil {
		return Package{}, "", managementError("EXAMPLE_UNAVAILABLE", "无法读取官方模板", 503)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return Package{}, "", managementError("EXAMPLE_UNAVAILABLE", "官方模板清单无效", 503)
	}
	source, err := fs.ReadFile(officialFS, path.Join(root, manifest.Main))
	if err != nil {
		return Package{}, "", managementError("EXAMPLE_UNAVAILABLE", "无法读取官方模板", 503)
	}
	pkg := Package{Plugin: data, Main: string(source)}
	dir, err := os.MkdirTemp("", "lintel-example-")
	if err != nil {
		return Package{}, "", managementError("EXAMPLE_UNAVAILABLE", "无法读取官方模板", 503)
	}
	defer os.RemoveAll(dir)
	if err := materialize(officialFS, root, dir); err != nil {
		return Package{}, "", managementError("EXAMPLE_UNAVAILABLE", "无法读取官方模板", 503)
	}
	canonical, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), append(canonical, '\n'), 0600); err != nil {
		return Package{}, "", managementError("EXAMPLE_UNAVAILABLE", "无法读取官方模板", 503)
	}
	hash, err := DigestDir(dir)
	if err != nil {
		return Package{}, "", managementError("EXAMPLE_UNAVAILABLE", "无法读取官方模板", 503)
	}
	return pkg, hash, nil
}
