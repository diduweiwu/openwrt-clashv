package core

import (
	"compress/gzip"
	"context"
	"encoding/json"
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
)

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// platformName 推导 mihomo Release 资产的平台名。
// 用户在设置里手填 CoreArch 时以手填为准（如 linux-mips-hardfloat）。
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
	st := CoreStatus{Path: path, Platform: platformName("")}
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

// UpgradeCore 下载最新 mihomo 并替换本地内核；内核在运行时会先停止、成功后重启。
// 返回新版本号。
func (m *Manager) UpgradeCore(ctx context.Context) (string, error) {
	rel, err := m.fetchRelease(ctx, coreRepo)
	if err != nil {
		return "", err
	}
	plat := platformName("")
	re, err := regexp.Compile(`^mihomo-` + regexp.QuoteMeta(plat) + `-v[\w.\-]+\.gz$`)
	if err != nil {
		return "", err
	}
	assetURL, ok := rel.findAsset(re)
	if !ok {
		return "", fmt.Errorf("最新版本 %s 没有 %s 平台的内核文件（可在设置里手动指定内核平台）", rel.TagName, plat)
	}
	slog.Info("开始下载内核", "version", rel.TagName, "url", assetURL)

	wasRunning := m.Running()
	if wasRunning {
		if err := m.Stop(); err != nil {
			return "", err
		}
	}
	if err := downloadToFile(ctx, m.proxiedURL(assetURL), m.cfg.CorePath(), true); err != nil {
		if wasRunning {
			_ = m.Start()
		}
		return "", err
	}
	slog.Info("内核已更新", "version", rel.TagName)
	if wasRunning {
		if err := m.Start(); err != nil {
			return rel.TagName, fmt.Errorf("内核已更新但启动失败: %w", err)
		}
	}
	return rel.TagName, nil
}

// downloadToFile 下载 URL 到目标路径；gz=true 时自动解压 gzip。
func downloadToFile(ctx context.Context, url, dst string, gz bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "clashv")
	hc := &http.Client{Timeout: 10 * time.Minute}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	var src io.Reader = io.LimitReader(resp.Body, 256<<20)
	tmp := dst + ".tmp"
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if gz {
		zr, err := gzip.NewReader(src)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("解压失败: %w", err)
		}
		defer zr.Close()
		src = zr
	}
	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
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
	self, err := os.Executable()
	if err != nil {
		return "", false, err
	}
	self, _ = filepath.Abs(self)
	if err := downloadToFile(ctx, m.proxiedURL(assetURL), self, false); err != nil {
		return "", false, err
	}
	slog.Info("插件已更新，等待服务重启", "version", rel.TagName, "path", self)
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
