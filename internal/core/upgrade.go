package core

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	coreRepo  = "MetaCubeX/mihomo"
	ghAPIBase = "https://api.github.com"
	testURL   = "https://www.gstatic.com/generate_204"

	maxAssetSize = 256 << 20 // 单文件下载上限
	dlRetryWait  = 2 * time.Second
)

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// platformName 推导 mihomo Release 资产的平台名。
// 用户在设置里选择/手填 CoreArch 时以设置为准（如 linux-mips-hardfloat），留空自动检测。
func platformName(s string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	if runtime.GOOS != "linux" {
		return runtime.GOOS + "-" + runtime.GOARCH
	}
	switch runtime.GOARCH {
	case "arm64":
		return "linux-arm64"
	case "amd64":
		return "linux-amd64-compatible" // 兼容版，老 CPU 也能跑
	case "arm":
		return "linux-armv7"
	case "mips":
		return "linux-mips-softfloat" // 常见 24kc 为软浮点，硬浮点请手动指定
	case "mipsle":
		return "linux-mipsle-softfloat"
	case "riscv64":
		return "linux-riscv64"
	default:
		return "linux-" + runtime.GOARCH
	}
}

// settingsArch 读取设置里用户指定的平台名（可能为空）。
func (m *Manager) settingsArch() string {
	s, err := m.cfg.Get()
	if err != nil {
		return ""
	}
	return s.CoreArch
}

// downloadCandidates 返回同一资源的候选地址：加速前缀优先，直连兜底。
// 镜像与 GitHub 的文件字节一致，中途切换可断点续传。
func (m *Manager) downloadCandidates(raw string) []string {
	var urls []string
	if p := m.proxiedURL(raw); p != raw {
		urls = append(urls, p)
	}
	return append(urls, raw)
}

// proxiedURL 给 GitHub 下载地址套上加速前缀（如 https://gh-proxy.com）。
// 仅处理 github.com 的资源链接；设置里留空则原样直连。
func (m *Manager) proxiedURL(raw string) string {
	s, err := m.cfg.Get()
	if err != nil || s.DownloadProxy == "" {
		return raw
	}
	prefix := strings.TrimSuffix(s.DownloadProxy, "/")
	if strings.HasPrefix(raw, "https://github.com/") || strings.HasPrefix(raw, "http://github.com/") {
		return prefix + "/" + raw
	}
	return raw
}

// fetchRelease 查询 GitHub 最新 Release：先直连 API，失败时改走下载加速前缀重试。
func (m *Manager) fetchRelease(ctx context.Context, repo string) (*ghRelease, error) {
	rel, err := fetchReleaseVia(ctx, ghAPIBase+"/repos/"+repo+"/releases/latest")
	if err == nil {
		return rel, nil
	}
	s, gerr := m.cfg.Get()
	if gerr == nil && s.DownloadProxy != "" {
		if rel2, err2 := fetchReleaseVia(ctx,
			strings.TrimSuffix(s.DownloadProxy, "/")+"/https://api.github.com/repos/"+repo+"/releases/latest"); err2 == nil {
			return rel2, nil
		}
	}
	return nil, err
}

func fetchReleaseVia(ctx context.Context, url string) (*ghRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clashv")
	req.Header.Set("Accept", "application/vnd.github+json")
	// 直连场景不走系统代理，加速前缀地址本身是可达端点
	hc := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("访问 GitHub 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API: HTTP %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func (r *ghRelease) findAsset(re *regexp.Regexp) (string, bool) {
	for _, a := range r.Assets {
		if re.MatchString(a.Name) {
			return a.BrowserDownloadURL, true
		}
	}
	return "", false
}

// ---- 升级进度 ----

// UpgradeProgress 一次升级任务的实时进度（内核/插件共用）。
type UpgradeProgress struct {
	Kind       string  `json:"kind"`        // core / plugin
	Stage      string  `json:"stage"`       // download / install / done / error
	Message    string  `json:"message"`     // 展示用文本（error 时为失败原因）
	Downloaded int64   `json:"downloaded"`  // 已下载字节
	Total      int64   `json:"total"`       // 总字节（服务器未给出时为 0）
	Percent    float64 `json:"percent"`     // 0-100，未知总量时为 0
	Active     bool    `json:"active"`      // 任务是否进行中
}

// beginUpgrade 占用一个升级槽位，同一时间只允许一个升级任务。
func (m *Manager) beginUpgrade(kind, message string) error {
	m.progMu.Lock()
	defer m.progMu.Unlock()
	if m.prog.Active {
		return fmt.Errorf("已有升级任务在进行中（%s），请稍后再试", m.prog.Kind)
	}
	m.prog = UpgradeProgress{Kind: kind, Stage: "download", Message: message, Active: true}
	return nil
}

// setProgress 更新进行中任务的进度；仅在任务活跃时生效。
func (m *Manager) setProgress(downloaded, total int64) {
	m.progMu.Lock()
	defer m.progMu.Unlock()
	if !m.prog.Active {
		return
	}
	m.prog.Downloaded = downloaded
	m.prog.Total = total
	if total > 0 {
		m.prog.Percent = float64(downloaded) / float64(total) * 100
		if m.prog.Percent > 100 {
			m.prog.Percent = 100
		}
	}
}

// finishProgress 更新任务阶段；done / error 为终态，其余阶段保持进行中。
func (m *Manager) finishProgress(stage, message string) {
	m.progMu.Lock()
	defer m.progMu.Unlock()
	m.prog.Stage = stage
	m.prog.Message = message
	switch stage {
	case "done", "error":
		m.prog.Active = false
	}
}

// Progress 返回当前升级进度快照。
func (m *Manager) Progress() UpgradeProgress {
	m.progMu.Lock()
	defer m.progMu.Unlock()
	return m.prog
}

// ---- 下载（多镜像轮询 + 断点续传） ----

// errRangeDone 表示续传起点已到文件末尾（HTTP 416）：本地 .tmp 可能已是完整文件。
var errRangeDone = errors.New("range already satisfied")

// downloadFile 把 candidates（同一文件、不同镜像）下载到 dst。
// 失败自动重试：保留 .tmp 断点，轮换镜像续传，直到成功或全部尝试耗尽。
func (m *Manager) downloadFile(ctx context.Context, candidates []string, dst string) error {
	if len(candidates) == 0 {
		return fmt.Errorf("没有可用的下载地址")
	}
	tmp := dst + ".tmp"
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	attempts := len(candidates) * 3 // 每个镜像首连 + 两次断点续传机会
	var lastErr error
	for i := 0; i < attempts; i++ {
		url := candidates[i%len(candidates)]
		resumed, _ := os.Stat(tmp)
		err := downloadOnce(ctx, url, tmp, m.setProgress)
		if err == nil {
			if rerr := os.Rename(tmp, dst); rerr != nil {
				return rerr
			}
			return nil
		}
		if errors.Is(err, errRangeDone) {
			// 已收满整个文件：gzip 校验通过即视为下载完成，否则丢弃重下
			if verifyGzip(tmp) == nil {
				if rerr := os.Rename(tmp, dst); rerr != nil {
					return rerr
				}
				return nil
			}
			os.Remove(tmp)
			lastErr = fmt.Errorf("断点文件校验失败，已重新下载")
			continue
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Warn("下载中断，准备重试", "attempt", fmt.Sprintf("%d/%d", i+1, attempts),
			"url", url, "had_bytes", sizeOf(resumed), "err", err)
		m.finishProgress("download", fmt.Sprintf("连接中断，自动重试 %d/%d", i+1, attempts))
		select {
		case <-time.After(dlRetryWait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	os.Remove(tmp)
	return fmt.Errorf("重试 %d 次后仍失败: %w", attempts, lastErr)
}

// verifyGzip 校验文件是否为完整可解压的 gzip 流。
func verifyGzip(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	_, err = io.Copy(io.Discard, zr)
	return err
}

func sizeOf(fi os.FileInfo) int64 {
	if fi == nil {
		return 0
	}
	return fi.Size()
}

// downloadOnce 单次下载尝试；tmp 已有部分数据且服务器支持 Range 时断点续传。
// onProg 周期性回报（已下载字节, 总字节）。
func downloadOnce(ctx context.Context, url, tmp string, onProg func(downloaded, total int64)) error {
	offset := int64(0)
	if st, err := os.Stat(tmp); err == nil {
		offset = st.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "clashv")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	hc := &http.Client{Timeout: 10 * time.Minute}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()

	appendMode := false
	switch {
	case resp.StatusCode == http.StatusPartialContent && offset > 0:
		appendMode = true // 续传成功
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0:
		return errRangeDone // 续传起点已达文件末尾，本地可能已完整
	case resp.StatusCode == http.StatusOK:
		offset = 0 // 服务器不支持 Range，从头下
	default:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength + offset
	flag := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(tmp, flag, 0o644)
	if err != nil {
		return err
	}

	downloaded := offset
	onProg(downloaded, total)
	pr := &progressReader{r: io.LimitReader(resp.Body, maxAssetSize), fn: func(n int) {
		downloaded += int64(n)
		onProg(downloaded, total)
	}}
	_, copyErr := io.Copy(f, pr)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("传输中断（已收 %s）: %w", humanBytes(downloaded), copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

// progressReader 边读边回调已读字节数。
type progressReader struct {
	r  io.Reader
	fn func(n int)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.fn(n)
	}
	return n, err
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// gunzipFile 解压 .gz 到 dst（经 .tmp 原子替换）。
func gunzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	zr, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("解压失败（文件不完整？）: %w", err)
	}
	defer zr.Close()
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, zr); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("解压失败（下载不完整？）: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// ---- 内核升级 ----

// CoreStatus 内核安装与版本信息。
type CoreStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Path      string `json:"path"`
	Platform  string `json:"platform"`
}

// CoreStatus 探测本地内核版本（优先问运行中的控制接口，否则执行 -v）。
func (m *Manager) CoreStatus(ctx context.Context) CoreStatus {
	path := m.cfg.CorePath()
	st := CoreStatus{Path: path, Platform: platformName(m.settingsArch())}
	if m.Running() {
		if v, err := m.hc.version(ctx); err == nil {
			st.Installed = true
			st.Version = v.Version
			return st
		}
	}
	if out, err := exec.Command(path, "-v").Output(); err == nil {
		line := strings.SplitN(string(out), "\n", 2)[0]
		st.Installed = true
		st.Version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Mihomo Meta"))
		if v := strings.Fields(st.Version); len(v) > 0 {
			// 形如 "Mihomo Meta v1.19.2 linux arm64 ..."，取版本号
			for _, f := range v {
				if strings.HasPrefix(f, "v") && strings.Contains(f, ".") {
					st.Version = f
					break
				}
			}
		}
	}
	return st
}

// LatestCore 查询 mihomo 最新版本。
func (m *Manager) LatestCore(ctx context.Context) (string, error) {
	rel, err := m.fetchRelease(ctx, coreRepo)
	if err != nil {
		return "", err
	}
	return rel.TagName, nil
}

// UpgradeCore 下载最新 mihomo 并替换本地内核；下载成功后才停内核替换，失败不影响运行中的内核。
// 返回新版本号。
func (m *Manager) UpgradeCore(ctx context.Context) (string, error) {
	s, err := m.cfg.Get()
	if err != nil {
		return "", err
	}
	rel, err := m.fetchRelease(ctx, coreRepo)
	if err != nil {
		return "", err
	}
	plat := platformName(s.CoreArch)
	re, err := regexp.Compile(`^mihomo-` + regexp.QuoteMeta(plat) + `-v[\w.\-]+\.gz$`)
	if err != nil {
		return "", err
	}
	assetURL, ok := rel.findAsset(re)
	if !ok {
		return "", fmt.Errorf("最新版本 %s 没有 %s 平台的内核文件（可在设置里手动指定内核平台）", rel.TagName, plat)
	}
	slog.Info("开始下载内核", "version", rel.TagName, "platform", plat, "url", assetURL)
	if err := m.beginUpgrade("core", "准备下载 "+rel.TagName); err != nil {
		return "", err
	}

	// 先下载（gz 原始文件，可断点续传），成功后再停内核做替换，把停机窗口压到最小
	gzPath := m.cfg.CorePath() + ".download.gz"
	if err := m.downloadFile(ctx, m.downloadCandidates(assetURL), gzPath); err != nil {
		m.finishProgress("error", "下载失败: "+err.Error())
		os.Remove(gzPath)
		return "", err
	}

	m.finishProgress("install", "下载完成，安装中…")
	wasRunning := m.Running()
	if wasRunning {
		if err := m.Stop(); err != nil {
			m.finishProgress("error", "停止内核失败: "+err.Error())
			os.Remove(gzPath)
			return "", err
		}
	}
	if err := gunzipFile(gzPath, m.cfg.CorePath()); err != nil {
		os.Remove(gzPath)
		m.finishProgress("error", err.Error())
		if wasRunning {
			_ = m.Start()
		}
		return "", err
	}
	os.Remove(gzPath)
	slog.Info("内核已更新", "version", rel.TagName)
	m.finishProgress("done", "已更新到 "+rel.TagName)
	if wasRunning {
		if err := m.Start(); err != nil {
			return rel.TagName, fmt.Errorf("内核已更新但启动失败: %w", err)
		}
	}
	return rel.TagName, nil
}

// ---- 插件自更新 ----

// LatestPlugin 查询插件仓库最新版本。
func (m *Manager) LatestPlugin(ctx context.Context) (string, error) {
	s, err := m.cfg.Get()
	if err != nil {
		return "", err
	}
	rel, err := m.fetchRelease(ctx, s.PluginRepo)
	if err != nil {
		return "", err
	}
	return rel.TagName, nil
}

// UpgradePlugin 下载新版本插件二进制并替换自身，替换后需重启服务生效。
// 返回 (新版本, 是否需要重启服务)。
func (m *Manager) UpgradePlugin(ctx context.Context) (string, bool, error) {
	s, err := m.cfg.Get()
	if err != nil {
		return "", false, err
	}
	rel, err := m.fetchRelease(ctx, s.PluginRepo)
	if err != nil {
		return "", false, err
	}
	plat := runtime.GOOS + "-" + runtime.GOARCH
	re := regexp.MustCompile(`^clashv-` + regexp.QuoteMeta(plat) + `(-v[\w.\-]+)?$`)
	assetURL, ok := rel.findAsset(re)
	if !ok {
		return rel.TagName, false, fmt.Errorf("最新版本 %s 没有 %s 的插件文件", rel.TagName, plat)
	}
	if err := m.beginUpgrade("plugin", "准备下载 "+rel.TagName); err != nil {
		return "", false, err
	}
	self, err := os.Executable()
	if err != nil {
		m.finishProgress("error", err.Error())
		return "", false, err
	}
	self, _ = filepath.Abs(self)
	if err := m.downloadFile(ctx, m.downloadCandidates(assetURL), self+".download"); err != nil {
		m.finishProgress("error", "下载失败: "+err.Error())
		os.Remove(self + ".download")
		return "", false, err
	}
	m.finishProgress("install", "下载完成，安装中…")
	if err := os.Chmod(self+".download", 0o755); err != nil {
		m.finishProgress("error", err.Error())
		os.Remove(self + ".download")
		return "", false, err
	}
	if err := os.Rename(self+".download", self); err != nil {
		m.finishProgress("error", err.Error())
		os.Remove(self + ".download")
		return "", false, err
	}
	slog.Info("插件已更新，等待服务重启", "version", rel.TagName, "path", self)
	m.finishProgress("done", "已更新到 "+rel.TagName)
	return rel.TagName, true, nil
}

// RestartService 通过 init 脚本重启自身服务（仅 OpenWrt 有效）。
// 延迟执行以保证 HTTP 响应先返回。
func (m *Manager) RestartService() error {
	if !m.cfg.IsOpenWrt() {
		return fmt.Errorf("非 OpenWrt 环境，请手动重启进程")
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = exec.Command("sh", "-c", "/etc/init.d/clashv restart >/dev/null 2>&1 &").Run()
	}()
	return nil
}
