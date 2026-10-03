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

// CreateBackup 把当前设置、订阅文件、自定义规则打包为 <name>.zip。
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
	if data, err := os.ReadFile(m.cfg.CustomRulesPath()); err == nil {
		files["custom-rules.txt"] = data
	}

	tmp := path + ".tmp"
	if err := writeZip(tmp, files); err != nil {
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
	slog.Info("已创建备份", "name", name, "profiles", len(profiles), "size", info.Size)
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

	// 先完整读出并校验 meta，再动手改状态——坏包不能造成「半恢复」
	var meta backupMeta
	var profileFiles, otherFiles map[string][]byte
	otherFiles = map[string][]byte{}
	for _, f := range zr.File {
		data, err := readZipFile(f)
		if err != nil {
			return false, fmt.Errorf("读取备份内容失败: %w", err)
		}
		switch {
		case f.Name == "meta.json":
			if err := json.Unmarshal(data, &meta); err != nil {
				return false, errors.New("备份元数据损坏，不是有效的备份文件")
			}
		case strings.HasPrefix(f.Name, "profiles/"):
			if profileFiles == nil {
				profileFiles = map[string][]byte{}
			}
			profileFiles[strings.TrimPrefix(f.Name, "profiles/")] = data
		case f.Name == "custom-rules.txt":
			otherFiles[f.Name] = data
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

func writeZip(path string, files map[string][]byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return zw.Close()
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
