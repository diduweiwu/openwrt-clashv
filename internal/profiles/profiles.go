// Package profiles 管理订阅：下载、存储、更新、删除。
//
// 存储布局: <workdir>/profiles/<id>.yaml   订阅原文（即 mihomo 配置）
//
//	<id>.meta.json        名称/URL/更新时间等元数据
package profiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"crypto/sha1"
	"encoding/hex"

	"clashv/internal/config"
)

// Profile 是一条订阅的元数据。
type Profile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	UpdatedAt int64  `json:"updated_at"`
	Size      int64  `json:"size"`
	UA        string `json:"ua,omitempty"` // 添加订阅时用的 User-Agent，更新时沿用
	// 订阅流量信息（来自 subscription-userinfo 响应头），0 表示机场未提供
	Upload   int64 `json:"upload,omitempty"`
	Download int64 `json:"download,omitempty"`
	Total    int64 `json:"total,omitempty"`
	Expire   int64 `json:"expire,omitempty"` // 到期时间 unix 秒
}

type metaFile struct {
	Profile
}

// Manager 管理订阅文件。
type Manager struct {
	cfg *config.Manager
	hc  *http.Client
}

// New 创建订阅管理器。
func New(cfg *config.Manager) *Manager {
	return &Manager{
		cfg: cfg,
		hc: &http.Client{
			Timeout: 60 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("重定向次数过多")
				}
				return nil
			},
		},
	}
}

func (m *Manager) yamlPath(id string) string {
	return filepath.Join(m.cfg.ProfilesDir(), id+".yaml")
}

func (m *Manager) metaPath(id string) string {
	return filepath.Join(m.cfg.ProfilesDir(), id+".meta.json")
}

// Get 读取单条订阅元数据。
func (m *Manager) Get(id string) (Profile, error) {
	if !validID(id) {
		return Profile{}, fmt.Errorf("非法的订阅 ID")
	}
	data, err := os.ReadFile(m.metaPath(id))
	if err != nil {
		return Profile{}, fmt.Errorf("订阅不存在: %s", id)
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return Profile{}, fmt.Errorf("订阅元数据损坏: %s", id)
	}
	return p, nil
}

// List 返回全部订阅，按更新时间倒序。
func (m *Manager) List() ([]Profile, error) {
	entries, err := os.ReadDir(m.cfg.ProfilesDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Profile
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.cfg.ProfilesDir(), e.Name()))
		if err != nil {
			continue
		}
		var p Profile
		if json.Unmarshal(data, &p) == nil && p.ID != "" {
			out = append(out, p)
		}
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].UpdatedAt > out[i].UpdatedAt {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// Path 返回订阅 yaml 文件路径（不存在时也返回期望路径）。
func (m *Manager) Path(id string) string { return m.yamlPath(id) }

// Add 下载 url 并保存为新订阅，返回元数据。ua 为空时用默认 User-Agent。
func (m *Manager) Add(name, url, ua string) (Profile, error) {
	name = strings.TrimSpace(name)
	url = strings.TrimSpace(url)
	if name == "" {
		name = "订阅"
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return Profile{}, fmt.Errorf("订阅地址必须以 http(s):// 开头")
	}
	data, info, err := m.download(url, ua)
	if err != nil {
		return Profile{}, err
	}
	id := newID(name, url)
	p := Profile{ID: id, Name: name, URL: url, UpdatedAt: time.Now().Unix(), Size: int64(len(data)), UA: strings.TrimSpace(ua)}
	p.Upload, p.Download, p.Total, p.Expire = info.upload, info.download, info.total, info.expire
	if err := m.write(p, data); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// Update 重新下载订阅内容（沿用添加时的 User-Agent）。
func (m *Manager) Update(id string) (Profile, error) {
	p, err := m.Get(id)
	if err != nil {
		return p, err
	}
	data, info, err := m.download(p.URL, p.UA)
	if err != nil {
		return p, err
	}
	p.UpdatedAt = time.Now().Unix()
	p.Size = int64(len(data))
	p.Upload, p.Download, p.Total, p.Expire = info.upload, info.download, info.total, info.expire
	if err := m.write(p, data); err != nil {
		return p, err
	}
	return p, nil
}

// Delete 删除订阅文件与元数据。
func (m *Manager) Delete(id string) error {
	if _, err := m.Get(id); err != nil {
		return err
	}
	for _, path := range []string{m.yamlPath(id), m.metaPath(id)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Edit 修改订阅的名称/地址/UA，返回更新后的元数据与是否重新下载了内容。
// ID 保持不变（激活状态不受影响）；地址或 UA 变化时重新下载——下载失败则整体
// 报错不落盘，避免改坏了连原配置都没了。名称留空时沿用原名称。
func (m *Manager) Edit(id, name, url, ua string) (Profile, bool, error) {
	p, err := m.Get(id)
	if err != nil {
		return p, false, err
	}
	name, url, ua = strings.TrimSpace(name), strings.TrimSpace(url), strings.TrimSpace(ua)
	if name == "" {
		name = p.Name
	}
	if url == "" {
		url = p.URL
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return Profile{}, false, fmt.Errorf("订阅地址必须以 http(s):// 开头")
	}
	redownload := url != p.URL || ua != p.UA
	p.Name, p.URL, p.UA = name, url, ua
	if redownload {
		data, info, err := m.download(url, ua)
		if err != nil {
			return Profile{}, false, err
		}
		p.UpdatedAt = time.Now().Unix()
		p.Size = int64(len(data))
		p.Upload, p.Download, p.Total, p.Expire = info.upload, info.download, info.total, info.expire
		if err := m.write(p, data); err != nil {
			return Profile{}, false, err
		}
	} else if err := m.writeMeta(p); err != nil {
		return Profile{}, false, err
	}
	return p, redownload, nil
}

func (m *Manager) write(p Profile, data []byte) error {
	if err := m.cfg.EnsureDirs(); err != nil {
		return err
	}
	tmpYaml := m.yamlPath(p.ID) + ".tmp"
	if err := os.WriteFile(tmpYaml, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpYaml, m.yamlPath(p.ID)); err != nil {
		return err
	}
	return m.writeMeta(p)
}

func (m *Manager) writeMeta(p Profile) error {
	meta, err := json.MarshalIndent(metaFile{p}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.metaPath(p.ID), meta, 0o644)
}

// subInfo 是机场返回的订阅流量信息（subscription-userinfo 头）。
type subInfo struct {
	upload   int64
	download int64
	total    int64
	expire   int64
}

// parseSubInfo 解析 "upload=123; download=456; total=789; expire=1750000000"。
func parseSubInfo(header string) subInfo {
	var info subInfo
	for _, part := range strings.Split(header, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(kv[0])) {
		case "upload":
			info.upload = n
		case "download":
			info.download = n
		case "total":
			info.total = n
		case "expire":
			info.expire = n
		}
	}
	return info
}

// download 拉取订阅内容并做最低限度校验（必须是 clash/mihomo 配置）。
// ua 为空时使用默认 UA；很多机场按 UA 决定返回的配置格式，需与添加时保持一致。
func (m *Manager) download(url, ua string) ([]byte, subInfo, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, subInfo{}, fmt.Errorf("订阅地址无效: %w", err)
	}
	if ua = strings.TrimSpace(ua); ua == "" {
		ua = "clash-verge/clashv"
	}
	req.Header.Set("User-Agent", ua)
	resp, err := m.hc.Do(req)
	if err != nil {
		return nil, subInfo{}, fmt.Errorf("下载订阅失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, subInfo{}, fmt.Errorf("下载订阅失败: HTTP %d", resp.StatusCode)
	}
	info := parseSubInfo(resp.Header.Get("subscription-userinfo"))
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, subInfo{}, fmt.Errorf("读取订阅失败: %w", err)
	}
	if len(data) == 0 {
		return nil, subInfo{}, fmt.Errorf("订阅内容为空")
	}
	head := string(data[:min(len(data), 4096)])
	if !strings.Contains(head, "proxies:") && !strings.Contains(head, "proxy-providers:") {
		return nil, subInfo{}, fmt.Errorf("订阅内容不是有效的 clash/mihomo 配置（缺少 proxies 段），请检查链接是否为订阅地址")
	}
	return data, info, nil
}

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

func newID(name, url string) string {
	h := sha1.Sum([]byte(name + "\x00" + url + "\x00" + time.Now().Format(time.RFC3339Nano)))
	return hex.EncodeToString(h[:])[:12]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
