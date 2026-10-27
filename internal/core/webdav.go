package core

// WebDAV 云备份：把本地备份包上传到用户的 WebDAV 网盘（坚果云、InfiniCloud、
// Alist、群晖等均兼容）。只用标准库 net/http + encoding/xml 实现 WebDAV 的
// 最小操作集：PROPFIND（连接测试/列目录）、MKCOL（逐级建目录）、PUT（上传）、
// GET（下载）、DELETE（删除），鉴权用 Basic Auth。

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"clashv/internal/config"
)

// 超时约定：连接测试/列目录等小请求 10 秒；上传/下载备份包（含内核可达数十 MB）
// 放宽到 10 分钟，与内核升级下载一致。
const (
	webdavQuickTimeout = 10 * time.Second
	webdavBulkTimeout  = 10 * time.Minute
)

// WebDAVClient 是面向某个远程备份目录的极简 WebDAV 客户端。
// 线程安全：字段只读，底层 http.Client 可并发使用。
type WebDAVClient struct {
	dirURL string // 备份目录完整 URL（无尾斜杠），如 https://dav.example.com/dav/clashv-backups
	user   string // Basic Auth 账号
	pass   string // Basic Auth 密码/应用密码
	http   *http.Client
}

// NewWebDAVClient 按服务器地址、账号密码与备份目录构造客户端。
// base 形如 https://dav.example.com/dav（可带尾斜杠）；dir 为相对备份目录，
// 支持多级路径（如 nas/clashv），非法字符会被剔除，空值回退默认目录。
// 地址缺失或无协议头时返回错误。
//
// 示例：
//
//	c, err := NewWebDAVClient("https://dav.jianguoyun.com/dav/", "a@b.c", "pass", "clashv 备份")
func NewWebDAVClient(base, user, pass, dir string) (*WebDAVClient, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || !strings.Contains(base, "://") {
		return nil, errors.New("WebDAV 地址无效，需以 http:// 或 https:// 开头")
	}
	dir = config.SanitizeWebDAVDir(dir)
	u := base + "/" + pathEscapeSegments(dir)
	return &WebDAVClient{
		dirURL: u,
		user:   user,
		pass:   pass,
		http:   &http.Client{},
	}, nil
}

// pathEscapeSegments 按路径段做 URL 编码：中文与空格转 %XX，多级分隔符 / 保留。
// 整串 url.PathEscape 会把 / 也转义掉，故逐段处理。
func pathEscapeSegments(dir string) string {
	parts := strings.Split(dir, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// newRequest 构造带 Basic Auth 的请求。
func (c *WebDAVClient) newRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if c.user != "" || c.pass != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	return req, nil
}

// do 发送请求并限定返回 2xx，其余状态读出服务端错误说明拼进错误信息。
func (c *WebDAVClient) do(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		// 读一小段响应体帮助定位（很多网盘会把原因写在 body 里）
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		reason := strings.TrimSpace(string(snippet))
		if reason == "" {
			reason = resp.Status
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return resp, errors.New("WebDAV 账号或密码错误（401）")
		case http.StatusForbidden:
			return resp, fmt.Errorf("WebDAV 拒绝访问（403）：%s", reason)
		default:
			return resp, fmt.Errorf("WebDAV 服务器返回 %s：%s", resp.Status, reason)
		}
	}
	return resp, nil
}

// Test 验证连通性：PROPFIND 备份目录，404（目录不存在）时自动逐级创建后复查。
// 全部通过意味着服务器可达、账号密码有效、备份目录可用。
func (c *WebDAVClient) Test() error {
	err := c.propfind(c.dirURL, 0)
	if err == nil {
		return nil
	}
	// 目录不存在的 404 走建目录重试；其余错误原样返回
	if !errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), "404") {
		return err
	}
	if err := c.EnsureDir(); err != nil {
		return err
	}
	return c.propfind(c.dirURL, 0)
}

// EnsureDir 从服务器根逐级 MKCOL 创建备份目录；405（已存在）视为成功。
func (c *WebDAVClient) EnsureDir() error {
	ctx, cancel := context.WithTimeout(context.Background(), webdavQuickTimeout)
	defer cancel()
	segs := strings.Split(c.dirURL, "/")
	// 从地址第三段（跳过 scheme:、空、host）开始逐级建
	cur := segs[0] + "//" + segs[2]
	for _, seg := range segs[3:] {
		if seg == "" {
			continue
		}
		cur += "/" + seg
		req, err := c.newRequest(ctx, "MKCOL", cur, nil)
		if err != nil {
			return err
		}
		resp, err := c.do(req)
		if resp != nil {
			resp.Body.Close()
		}
		if err != nil && !strings.Contains(err.Error(), "405") {
			return fmt.Errorf("创建远程目录失败: %w", err)
		}
	}
	return nil
}

// propfind 对指定 URL 发 PROPFIND（Depth 按 d），非 2xx 转为错误（404 特殊：
// 错误信息带 404 供调用方识别目录缺失）。
func (c *WebDAVClient) propfind(target string, depth int) error {
	ctx, cancel := context.WithTimeout(context.Background(), webdavQuickTimeout)
	defer cancel()
	req, err := c.newRequest(ctx, "PROPFIND", target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Depth", fmt.Sprint(depth))
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}

// WebDAVFile 是远程目录里的一条备份包信息。
type WebDAVFile struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt int64  `json:"created_at"` // 远程 last-modified，Unix 秒；服务器未提供时为 0
}

// List 列出备份目录下的全部文件（PROPFIND Depth 1），剔除目录本身与子目录。
func (c *WebDAVClient) List() ([]WebDAVFile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), webdavQuickTimeout)
	defer cancel()
	req, err := c.newRequest(ctx, "PROPFIND", c.dirURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", "1")
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var ms webdavMultistatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("解析 WebDAV 目录列表失败: %w", err)
	}
	self := c.dirURL + "/"
	var out []WebDAVFile
	for _, r := range ms.Responses {
		name, isDir, size, modified := r.props()
		if name == "" || isDir || strings.HasSuffix(r.Href, self) {
			continue // 目录自身与子目录不算备份文件
		}
		out = append(out, WebDAVFile{Name: name, Size: size, CreatedAt: modified})
	}
	return out, nil
}

// webdavMultistatus 是 PROPFIND 响应的 XML 骨架（只取关心的三个属性；
// 各家网盘命名空间前缀不一，encoding/xml 按本地名匹配，无需关心前缀）。
type webdavMultistatus struct {
	Responses []webdavResponse `xml:"response"`
}

type webdavResponse struct {
	Href     string `xml:"href"`
	Propstat []struct {
		Prop struct {
			ContentLength int64  `xml:"getcontentlength"`
			LastModified  string `xml:"getlastmodified"`
			ResourceType  struct {
				Collection []byte `xml:"collection"`
			} `xml:"resourcetype"`
		} `xml:"prop"`
		Status string `xml:"status"`
	} `xml:"propstat"`
}

// props 从 response 里取文件名、是否目录、大小与修改时间。
// href 可能是相对路径也可能是完整 URL、还可能 URL 编码过，统一取末段并解码。
func (r *webdavResponse) props() (name string, isDir bool, size int64, modified int64) {
	href := r.Href
	if i := strings.Index(href, "://"); i >= 0 {
		href = href[i+3:]
		href = href[strings.IndexByte(href, '/'):] // 去掉 host，留路径
	}
	name = path.Base(strings.TrimSuffix(href, "/"))
	if dec, err := url.PathUnescape(name); err == nil {
		name = dec
	}
	for _, ps := range r.Propstat {
		if ps.Status != "" && !strings.Contains(ps.Status, "200") {
			continue // HTTP/1.1 404 Not Found 的 propstat（属性缺失）跳过
		}
		if len(ps.Prop.ResourceType.Collection) > 0 {
			isDir = true
		}
		size = ps.Prop.ContentLength
		if t, err := http.ParseTime(ps.Prop.LastModified); err == nil {
			modified = t.Unix()
		}
	}
	return name, isDir, size, modified
}

// Upload 把本地文件 srcPath 流式上传为远程备份目录下的 name（不整块占内存）。
func (c *WebDAVClient) Upload(name string, srcPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), webdavBulkTimeout)
	defer cancel()
	req, err := c.newRequest(ctx, "PUT", c.dirURL+"/"+pathEscapeSegments(name), f)
	if err != nil {
		return err
	}
	req.ContentLength = st.Size()
	resp, err := c.do(req)
	if resp != nil {
		resp.Body.Close()
	}
	return err
}

// Download 把远程文件 name 流式下载到本地 dstPath（先写临时文件再原子改名，
// 失败不残留半个坏文件）。
func (c *WebDAVClient) Download(name, dstPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), webdavBulkTimeout)
	defer cancel()
	req, err := c.newRequest(ctx, "GET", c.dirURL+"/"+pathEscapeSegments(name), nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	tmp := dstPath + ".dav.tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("下载备份失败: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dstPath)
}

// Delete 删除远程文件 name。
func (c *WebDAVClient) Delete(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), webdavQuickTimeout)
	defer cancel()
	req, err := c.newRequest(ctx, "DELETE", c.dirURL+"/"+pathEscapeSegments(name), nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if resp != nil {
		resp.Body.Close()
	}
	return err
}

// ---- 备份与恢复的云上流程（挂 Manager，配置来自已保存设置）----

// webdavClientFromConfig 按当前已保存的 WebDAV 设置构造客户端；未配置地址时报错。
func (m *Manager) webdavClientFromConfig() (*WebDAVClient, error) {
	s, err := m.cfg.Get()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.WebDAVURL) == "" {
		return nil, errors.New("尚未配置 WebDAV 服务器地址")
	}
	return NewWebDAVClient(s.WebDAVURL, s.WebDAVUser, s.WebDAVPass, s.WebDAVDir)
}

// WebDAVTest 用传入的连接参数做一次连通性测试（供「连接测试」按钮在保存前
// 验证表单当前值），目录不存在时会顺带创建。成功返回耗时（毫秒）。
func (m *Manager) WebDAVTest(base, user, pass, dir string) (int64, error) {
	c, err := NewWebDAVClient(base, user, pass, dir)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	if err := c.Test(); err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

// WebDAVBackupCreate 打包当前全部状态（同 CreateBackup，名称末尾自动追加随机串）
// 并上传到 WebDAV 备份目录；上传成功后删除本地临时包，失败则保留，可从本地
// 备份列表找到并重试。
func (m *Manager) WebDAVBackupCreate(name string) (BackupInfo, error) {
	c, err := m.webdavClientFromConfig()
	if err != nil {
		return BackupInfo{}, err
	}
	info, err := m.CreateBackup(name)
	if err != nil {
		return BackupInfo{}, err
	}
	local := m.backupsDir() + "/" + info.Name + ".zip"
	if err := c.Upload(info.Name+".zip", local); err != nil {
		return BackupInfo{}, fmt.Errorf("备份包已生成但上传失败（本地已保留一份）: %w", err)
	}
	if err := os.Remove(local); err != nil {
		slog.Warn("云备份已上传，清理本地临时包失败", "name", info.Name, "err", err)
	}
	slog.Info("已上传 WebDAV 备份", "name", info.Name, "size", info.Size)
	return info, nil
}

// WebDAVBackupList 列出远程备份目录里的备份包（仅 .zip），按时间倒序。
func (m *Manager) WebDAVBackupList() ([]BackupInfo, error) {
	c, err := m.webdavClientFromConfig()
	if err != nil {
		return nil, err
	}
	files, err := c.List()
	if err != nil {
		return nil, err
	}
	out := make([]BackupInfo, 0, len(files))
	for _, f := range files {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".zip") {
			continue
		}
		out = append(out, BackupInfo{
			Name:      strings.TrimSuffix(f.Name, ".zip"),
			Size:      f.Size,
			CreatedAt: f.CreatedAt,
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

// WebDAVBackupRestore 下载远程备份到本地备份目录（本地留档，下次可直接恢复）
// 并复用 RestoreBackup 覆盖当前全部状态；内核在运行则自动重启。返回是否发生了重启。
func (m *Manager) WebDAVBackupRestore(name string) (bool, error) {
	name, err := sanitizeBackupName(name)
	if err != nil {
		return false, err
	}
	c, err := m.webdavClientFromConfig()
	if err != nil {
		return false, err
	}
	local := m.backupsDir() + "/" + name + ".zip"
	if err := os.MkdirAll(m.backupsDir(), 0o755); err != nil {
		return false, err
	}
	if err := c.Download(name+".zip", local); err != nil {
		return false, err
	}
	return m.RestoreBackup(name)
}

// WebDAVBackupDelete 删除远程备份（本地留档不受影响）。
func (m *Manager) WebDAVBackupDelete(name string) error {
	name, err := sanitizeBackupName(name)
	if err != nil {
		return err
	}
	c, err := m.webdavClientFromConfig()
	if err != nil {
		return err
	}
	return c.Delete(name + ".zip")
}
