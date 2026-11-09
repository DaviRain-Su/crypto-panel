package authoring

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	localIDPattern = regexp.MustCompile(`^local\.[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)+$`)
	hashPattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func personalID(id string) bool {
	return len(id) <= 128 && strings.HasPrefix(id, "local.personal.") && localIDPattern.MatchString(id)
}
func absolutePath(name string) bool {
	return filepath.IsAbs(name) && filepath.Clean(name) == name && !strings.ContainsAny(name, "\x00\r\n") && !strings.Contains(name, "://")
}
func jsonObject(data []byte) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(data, &object) == nil && object != nil
}

func validateRequest(req Request) error {
	if strings.TrimSpace(req.Prompt) == "" || !utf8.ValidString(req.Prompt) || len(req.Prompt) > maxPromptBytes {
		return ErrInvalidRequest
	}
	if req.WorkspaceRoot != "" && !absolutePath(req.WorkspaceRoot) {
		return ErrInvalidRequest
	}
	if len(req.Catalog) > maxCatalogBytes || !jsonObject(req.Catalog) || len(req.Examples) == 0 || len(req.Examples) > maxExamples {
		return ErrInvalidRequest
	}
	total := len(req.Catalog)
	for _, example := range req.Examples {
		if len(example.Plugin) > maxManifestBytes || len(example.Main) > maxMainBytes || !jsonObject(example.Plugin) {
			return ErrInvalidRequest
		}
		data, err := json.Marshal(example)
		if err != nil {
			return ErrInvalidRequest
		}
		total += len(data)
		if total > maxContextBytes {
			return ErrInvalidRequest
		}
	}
	if len(req.ConfigValues) > maxConfigValues {
		return ErrInvalidRequest
	}
	for key, value := range req.ConfigValues {
		if key == "" || len(key) > maxConfigValueBytes || len(value) > maxConfigValueBytes || !utf8.ValidString(key) || !utf8.ValidString(value) {
			return ErrInvalidRequest
		}
	}
	if req.ExistingDir != "" {
		if !absolutePath(req.ExistingDir) || len(req.TargetID) > 128 || !localIDPattern.MatchString(req.TargetID) || !hashPattern.MatchString(req.ExpectedHash) {
			return ErrInvalidRequest
		}
	} else if req.ExpectedHash != "" || (req.TargetID != "" && !personalID(req.TargetID)) {
		return ErrInvalidRequest
	}
	return nil
}

func localProfile(profile Profile) (Profile, error) {
	if strings.TrimSpace(profile.Executable) == "" || strings.ContainsAny(profile.Executable, "\x00\r\n") {
		return Profile{}, ErrExecutable
	}
	if forbiddenExecutable(profile.Executable) {
		return Profile{}, ErrUnsafeExecutable
	}
	resolved, err := exec.LookPath(profile.Executable)
	if err != nil {
		return Profile{}, ErrExecutable
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return Profile{}, ErrExecutable
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return Profile{}, ErrExecutable
	}
	if forbiddenExecutable(resolved) {
		return Profile{}, ErrUnsafeExecutable
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return Profile{}, ErrExecutable
	}
	for _, arg := range profile.Args {
		if strings.ContainsRune(arg, 0) {
			return Profile{}, ErrInvalidRequest
		}
	}
	profile.Executable = resolved
	profile.Args = append([]string(nil), profile.Args...)
	return profile, nil
}

func forbiddenExecutable(executable string) bool {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(executable)), ".exe")
	switch base {
	case "npx", "npx-cli.js", "uvx", "curl", "wget", "npm", "npm-cli.js", "pnpm", "yarn", "uv", "pip", "pip3", "pipx", "brew", "apt", "apt-get", "sh", "bash", "zsh", "dash", "fish", "csh", "tcsh", "env", "cmd", "powershell", "pwsh":
		return true
	}
	return false
}

// 只从允许表逐项读取；不遍历 Environ，避免任意 token 或 SECRET 被隐式传入。
func safeEnvironment() []string {
	env := make([]string, 0, 6)
	for _, key := range []string{"PATH", "HOME", "USER", "TMPDIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		value, ok := os.LookupEnv(key)
		if ok || key == "PATH" || key == "HOME" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func createWorkspace(ctx context.Context, parent string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	explicitRoot := strings.TrimSpace(parent) != ""
	if parent == "" {
		parent = filepath.Clean(os.TempDir())
	}
	if !absolutePath(parent) {
		return "", ErrInvalidRequest
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", ErrWorkspace
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return "", ErrWorkspace
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return "", ErrWorkspace
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return "", ErrWorkspace
	}
	if explicitRoot {
		_ = SweepStaleWorkspaces(parent)
	}
	for range 10 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", ErrWorkspace
		}
		name := "lintel-authoring-" + hex.EncodeToString(nonce[:])
		err := root.Mkdir(name, 0700)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", ErrWorkspace
		}
		current, err := os.Lstat(parent)
		if err != nil || !os.SameFile(info, current) || current.Mode()&fs.ModeSymlink != 0 {
			_ = root.RemoveAll(name)
			return "", ErrWorkspace
		}
		return filepath.Join(parent, name), nil
	}
	return "", ErrWorkspace
}

// 在创建目录前阻止把工作根放到原应用内部；创建后再次检查实际工作目录。
func checkWorkspaceOverlap(workspace, source string) error {
	if workspace == "" {
		workspace = filepath.Clean(os.TempDir())
	}
	src, err := filepath.EvalSymlinks(source)
	if err != nil {
		return ErrSource
	}
	resolved, err := canonicalParent(workspace)
	if err != nil {
		return ErrWorkspace
	}
	if within(src, resolved) {
		return ErrSource
	}
	return nil
}

func canonicalParent(name string) (string, error) {
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(name)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) || filepath.Dir(name) == name {
			return "", err
		}
		suffix = append(suffix, filepath.Base(name))
		name = filepath.Dir(name)
	}
}
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)))
}

func writeContext(ctx context.Context, workspace string, req Request) error {
	dir := filepath.Join(workspace, "context")
	if err := os.Mkdir(dir, 0700); err != nil {
		return ErrWorkspace
	}
	write := func(name string, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0400); err != nil {
			return ErrWorkspace
		}
		return nil
	}
	if err := write("catalog.json", req.Catalog); err != nil {
		return err
	}
	for i, example := range req.Examples {
		data, err := json.Marshal(example)
		if err != nil {
			return ErrInvalidRequest
		}
		if err = write(fmtExampleName(i+1), data); err != nil {
			return err
		}
	}
	if err := os.Chmod(dir, 0500); err != nil {
		return ErrWorkspace
	}
	return nil
}

func fmtExampleName(i int) string { return "example-" + strconv.Itoa(i) + ".cryptoapp.json" }

// SweepStaleWorkspaces 删除 parent 下上次进程留下的 lintel-authoring-* 目录。
// 未指定工作根时直接返回，绝不清扫系统临时目录。
// 普通文件和其他目录保留。context 目录是只读的，删除前先恢复写权限。
func SweepStaleWorkspaces(parent string) error {
	if strings.TrimSpace(parent) == "" {
		return nil
	}
	entries, err := os.ReadDir(parent)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "lintel-authoring-") || strings.Contains(name, "..") {
			continue
		}
		info, err := os.Lstat(filepath.Join(parent, name))
		if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		removeWorkspace(filepath.Join(parent, name))
	}
	return nil
}

func removeWorkspace(dir string) {
	if dir == "" {
		return
	}
	_ = filepath.WalkDir(dir, func(name string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(name, 0700)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
}

// copyExisting 只复制普通代码 / 资源；排除项先检查类型，但绝不打开其内容。
func copyExisting(ctx context.Context, source, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return ErrSource
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return ErrSource
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return ErrSource
	}
	dest, err := os.OpenRoot(destination)
	if err != nil {
		return ErrWorkspace
	}
	defer dest.Close()
	return copyCodeTree(ctx, root, dest, &copyBudget{})
}

type copyBudget struct {
	files, entries int
	bytes          int64
}

// 每层固定目录句柄并分批枚举，避免中间目录被换成树内链接或一次枚举无限条目。
func copyCodeTree(ctx context.Context, source, dest *os.Root, budget *copyBudget) error {
	directory, err := source.Open(".")
	if err != nil {
		return ErrSource
	}
	defer directory.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := directory.ReadDir(64)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return ErrSource
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			budget.entries++
			if budget.entries > maxCopyEntries {
				return ErrSourceLimit
			}
			name := entry.Name()
			if !fs.ValidPath(name) || path.Base(name) != name || strings.ContainsAny(name, "\\\x00\r\n") {
				return ErrSource
			}
			info, err := source.Lstat(name)
			if err != nil || info.Mode()&fs.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) || entry.IsDir() != info.IsDir() {
				return ErrSource
			}
			if excludedCodePath(name) {
				continue
			}
			if info.IsDir() {
				if err := copyCodeDirectory(ctx, source, dest, name, info, budget); err != nil {
					return err
				}
				continue
			}
			budget.files++
			if budget.files > maxCopyFiles || info.Size() > maxCopyFileBytes {
				return ErrSourceLimit
			}
			data, err := readCodeFile(ctx, source, name, info)
			if err != nil {
				return err
			}
			budget.bytes += int64(len(data))
			if budget.bytes > maxCopyTotalBytes {
				return ErrSourceLimit
			}
			f, err := dest.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return ErrWorkspace
			}
			_, err = f.Write(data)
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				return ErrWorkspace
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
	}
}

func copyCodeDirectory(ctx context.Context, source, dest *os.Root, name string, info fs.FileInfo, budget *copyBudget) error {
	child, err := source.OpenRoot(name)
	if err != nil {
		return ErrSource
	}
	defer child.Close()
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return ErrSource
	}
	if err := dest.Mkdir(name, 0700); err != nil {
		return ErrWorkspace
	}
	output, err := dest.OpenRoot(name)
	if err != nil {
		return ErrWorkspace
	}
	defer output.Close()
	return copyCodeTree(ctx, child, output, budget)
}

func readCodeFile(ctx context.Context, root *os.Root, name string, info fs.FileInfo) ([]byte, error) {
	f, err := openCodeFile(root, name)
	if err != nil {
		return nil, ErrSource
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, ErrSource
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCopyFileBytes+1))
	if err != nil {
		return nil, ErrSource
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > maxCopyFileBytes {
		return nil, ErrSourceLimit
	}
	return data, nil
}

func excludedCodePath(rel string) bool {
	name := strings.ToLower(path.Base(rel))
	switch name {
	case "state.json", "kv.json", "install.json", "settings.json", "secrets.json", "credentials.json", "token.json", "tokens.json", ".seeded-official", "settings", "secrets", "credentials", ".git", ".ssh", ".aws", ".gnupg", ".config", ".claude", ".codex", ".lifecycle", ".ds_store", "thumbs.db", "__macosx", ".env", ".npmrc", ".netrc":
		return true
	}
	if strings.HasPrefix(name, ".env.") || strings.HasPrefix(name, ".install-") || strings.HasPrefix(name, ".official-") {
		return true
	}
	switch path.Ext(name) {
	case ".pem", ".key", ".p12", ".pfx":
		return true
	}
	return false
}

// 此处只比对被选目录的身份，不代替 Manager 的清单与代码校验。
func checkCopiedTarget(app, target string) error {
	data, err := os.ReadFile(filepath.Join(app, "plugin.json"))
	if err != nil || len(data) > maxManifestBytes {
		return ErrSource
	}
	var identity struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &identity) != nil || identity.ID != target {
		return ErrSource
	}
	return nil
}
