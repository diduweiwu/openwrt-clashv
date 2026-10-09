package core

// 备份与恢复：把插件全部可持久化状态打成单个 zip 存到 <home>/backups/。
// 内容 = 设置（含激活订阅选择）+ 全部订阅文件与元数据 + 自定义规则；
// 不含内核二进制与运行时缓存（cache.db/config.yaml/core.state，它们由内核
// 按当前配置自动重建）。恢复时写回以上全部内容并按需重启内核。

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"clashv/internal/config"
	"clashv/internal/templates"
)

// BackupInfo 是一条备份记录（列表展示用）。
type BackupInfo struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt int64  `json:"created_at"` // Unix 秒
}

// backupMeta 是备份包内的元数据文件。
type backupMeta struct {
	Version   string          `json:"version"`
	CreatedAt time.Time       `json:"created_at"`
	Settings  config.Settings `json:"settings"`
}

var backupNameRe = regexp.MustCompile(`[\\/:*?"<>|]`)

// sanitizeBackupName 规整用户输入的备份名：去空白、剔除文件系统非法字符、
// 限长。中文名允许。返回不带 .zip 后缀的名字。
func sanitizeBackupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = backupNameRe.ReplaceAllString(name, "")
	name = strings.Trim(name, ".")
	if name == "" {
		return "", errors.New("备份名不能为空")
	}
	r := []rune(name)
	if len(r) > 80 {
		name = string(r[:80])
	}
	return name, nil
}

// backupsDir 返回备份目录。
func (m *Manager) backupsDir() string { return filepath.Join(m.cfg.Home(), "backups") }

// CreateBackup 把当前设置、订阅文件、自定义规则、内核二进制打包为 <name>.zip。
func (m *Manager) CreateBackup(name string) (BackupInfo, error) {
	name, err := sanitizeBackupName(name)
	if err != nil {
		return BackupInfo{}, err
	}
	dir := m.backupsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return BackupInfo{}, err
	}
	path := filepath.Join(dir, name+".zip")
	if _, err := os.Stat(path); err == nil {
		return BackupInfo{}, fmt.Errorf("同名备份「%s」已存在", name)
	}

	files := map[string][]byte{}
	s, err := m.cfg.Get()
	if err != nil {
		return BackupInfo{}, err
	}
	meta, err := json.Marshal(backupMeta{Version: m.PluginVersion, CreatedAt: time.Now(), Settings: s})
	if err != nil {
		return BackupInfo{}, err
	}
	files["meta.json"] = meta
	profiles, _ := m.prof.List()
	for _, p := range profiles {
		if data, err := os.ReadFile(m.prof.Path(p.ID)); err == nil {
			files["profiles/"+p.ID+".yaml"] = data
		}
		if data, err := os.ReadFile(filepath.Join(m.cfg.ProfilesDir(), p.ID+".meta.json")); err == nil {
			files["profiles/"+p.ID+".meta.json"] = data
		}
	}
	// 配置模板（手动节点订阅依赖，不备份会丢生成来源）
	if tpls, err := templates.New(m.cfg).List(); err == nil {
		for _, t := range tpls {
			if data, err := os.ReadFile(filepath.Join(m.cfg.Home(), "templates", t.Name+".yaml")); err == nil {
				files["templates/"+t.Name+".yaml"] = data
			}
		}
	}
	if data, err := os.ReadFile(m.cfg.CustomRulesPath()); err == nil {
		files["custom-rules.txt"] = data
	}

	// 内核二进制（十几~几十 MB）流式写入，不整块进内存
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return BackupInfo{}, err
	}
	zw := zip.NewWriter(f)
	for n, data := range files {
		w, err := zw.Create(n)
		if err == nil {
			_, err = w.Write(data)
		}
		if err != nil {
			zw.Close()
			f.Close()
			_ = os.Remove(tmp)
			return BackupInfo{}, err
		}
	}
	cf, err := os.Open(m.cfg.CorePath())
	if err != nil {
		slog.Info("内核未安装，备份不含内核程序", "path", m.cfg.CorePath())
	} else {
		w, err := zw.Create("core.bin")
		if err == nil {
			_, err = io.Copy(w, cf)
		}
		cf.Close()
		if err != nil {
			zw.Close()
			f.Close()
			_ = os.Remove(tmp)
			return BackupInfo{}, err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return BackupInfo{}, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return BackupInfo{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return BackupInfo{}, err
	}
	st, _ := os.Stat(path)
	info := BackupInfo{Name: name, CreatedAt: time.Now().Unix()}
	if st != nil {
		info.Size = st.Size()
	}
	slog.Info("已创建备份", "name", name, "profiles", len(profiles), "with_core", cf == nil, "size", info.Size)
	return info, nil
}

// ListBackups 返回全部备份，按创建时间倒序。
func (m *Manager) ListBackups() ([]BackupInfo, error) {
	entries, err := os.ReadDir(m.backupsDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []BackupInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupInfo{
			Name:      strings.TrimSuffix(e.Name(), ".zip"),
			Size:      st.Size(),
			CreatedAt: st.ModTime().Unix(),
		})
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt > out[i].CreatedAt {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// DeleteBackup 删除一份备份。
func (m *Manager) DeleteBackup(name string) error {
	name, err := sanitizeBackupName(name)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(m.backupsDir(), name+".zip")); err != nil {
		return fmt.Errorf("备份不存在或已删除: %w", err)
	}
	return nil
}

// RestoreBackup 用备份覆盖当前全部状态：设置、订阅文件、自定义规则。
// 内核在运行则恢复后自动重启（重新合成配置），返回是否发生了重启。
func (m *Manager) RestoreBackup(name string) (bool, error) {
	name, err := sanitizeBackupName(name)
	if err != nil {
		return false, err
	}
	zr, err := zip.OpenReader(filepath.Join(m.backupsDir(), name+".zip"))
	if err != nil {
		return false, fmt.Errorf("备份不存在或已损坏: %w", err)
	}
	defer zr.Close()

	// 先完整读出并校验 meta，再动手改状态——坏包不能造成「半恢复」。
	// 内核二进制只记句柄，校验通过后流式解出，不整块占内存
	var meta backupMeta
	var profileFiles, otherFiles map[string][]byte
	var coreEntry *zip.File
	otherFiles = map[string][]byte{}
	for _, f := range zr.File {
		switch {
		case f.Name == "meta.json":
			data, err := readZipFile(f)
			if err != nil {
				return false, fmt.Errorf("读取备份内容失败: %w", err)
			}
			if err := json.Unmarshal(data, &meta); err != nil {
				return false, errors.New("备份元数据损坏，不是有效的备份文件")
			}
		case strings.HasPrefix(f.Name, "profiles/"):
			data, err := readZipFile(f)
			if err != nil {
				return false, fmt.Errorf("读取备份内容失败: %w", err)
			}
			if profileFiles == nil {
				profileFiles = map[string][]byte{}
			}
			profileFiles[strings.TrimPrefix(f.Name, "profiles/")] = data
		case strings.HasPrefix(f.Name, "templates/"):
			data, err := readZipFile(f)
			if err != nil {
				return false, fmt.Errorf("读取备份内容失败: %w", err)
			}
			otherFiles[f.Name] = data
		case f.Name == "custom-rules.txt":
			data, err := readZipFile(f)
			if err != nil {
				return false, fmt.Errorf("读取备份内容失败: %w", err)
			}
			otherFiles[f.Name] = data
		case f.Name == "core.bin":
			coreEntry = f
		}
	}
	if meta.CreatedAt.IsZero() {
		return false, errors.New("备份元数据损坏，不是有效的备份文件")
	}

	// 落盘：停内核、清运行缓存，重建订阅目录
	wasRunning := m.Running()
	if err := m.Stop(); err != nil {
		slog.Warn("恢复备份：停止内核失败，继续恢复", "err", err)
	}
	for _, f := range []string{"core.state", "cache.db", "config.yaml"} {
		_ = os.Remove(filepath.Join(m.cfg.Home(), f))
	}
	profDir := m.cfg.ProfilesDir()
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		return false, err
	}
	if old, err := os.ReadDir(profDir); err == nil {
		for _, e := range old {
			if !e.IsDir() {
				_ = os.Remove(filepath.Join(profDir, e.Name()))
			}
		}
	}
	for name, data := range profileFiles {
		if strings.Contains(name, "..") || strings.Contains(name, "/") {
			continue // zip 内路径防御：只接受平铺的文件名
		}
		if err := os.WriteFile(filepath.Join(profDir, name), data, 0o644); err != nil {
			return false, err
		}
	}
	// 模板：整目录重建（旧备份没有 templates/ 则只保留内置播种）
	tplDir := filepath.Join(m.cfg.Home(), "templates")
	if err := os.MkdirAll(tplDir, 0o755); err != nil {
		return false, err
	}
	if old, err := os.ReadDir(tplDir); err == nil {
		for _, e := range old {
			if !e.IsDir() {
				_ = os.Remove(filepath.Join(tplDir, e.Name()))
			}
		}
	}
	for name, data := range otherFiles {
		if !strings.HasPrefix(name, "templates/") {
			continue
		}
		base := strings.TrimPrefix(name, "templates/")
		if base == "" || strings.Contains(base, "..") || strings.Contains(base, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(tplDir, base), data, 0o644); err != nil {
			return false, err
		}
	}
	_ = templates.New(m.cfg).EnsureBuiltin() // 内置模板自愈兜底
	// 备份里带了内核就写回当前内核路径（0o755 可执行）；旧备份没有则保留现状
	if coreEntry != nil {
		corePath := m.cfg.CorePath()
		if err := os.MkdirAll(filepath.Dir(corePath), 0o755); err != nil {
			return false, err
		}
		if err := extractZipFile(coreEntry, corePath, 0o755); err != nil {
			return false, fmt.Errorf("恢复内核程序失败: %w", err)
		}
	}
	if data, ok := otherFiles["custom-rules.txt"]; ok {
		if err := m.cfg.SetCustomRules(strings.Split(string(data), "\n")); err != nil {
			return false, err
		}
	} else {
		_ = os.Remove(m.cfg.CustomRulesPath())
	}
	// 设置整体回写为备份版本；WorkDir 是本机运行参数，保留当前值
	// （Update 持有配置锁，Home() 不能在闭包里调，先取好）
	home := m.cfg.Home()
	if err := m.cfg.Update(func(u *config.Settings) {
		*u = meta.Settings
		u.WorkDir = home
	}); err != nil {
		return false, err
	}
	slog.Info("备份已恢复", "name", name, "profiles", len(profileFiles), "backup_version", meta.Version)

	if wasRunning {
		if err := m.Restart(); err != nil {
			slog.Warn("恢复备份后重启内核失败", "err", err)
			return false, nil // 恢复本身成功，重启失败让用户手动启动
		}
		return true, nil
	}
	return false, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// extractZipFile 把 zip 条目流式解到目标路径（大文件如内核二进制不占内存）。
func extractZipFile(f *zip.File, dst string, mode os.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp := dst + ".restore.tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
