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

// ghAsset Release 附件：URL 是 assets API 端点（私有仓库下载必须走它），
// BrowserDownloadURL 是公开仓库的普通下载链接。
type ghAsset struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type ghRelease struct {
	ID      int64     `json:"id"` // GitHub 按创建顺序单调递增，可用于比较新旧
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

// httpStatusError 带 HTTP 状态码的 API 错误，供调用方按状态码定制提示。
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("GitHub API: HTTP %d", e.code)
}

// platformName 推导 mihomo Release 资产的平台名，完全自动识别；
// 未知架构返回空字符串（视为不支持，不提供自动下载）。
func platformName() string {
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
		return "linux-mips-softfloat" // 软浮点兼容硬浮点 CPU，反向不行
	case "mipsle":
		return "linux-mipsle-softfloat"
	case "riscv64":
		return "linux-riscv64"
	case "loong64":
		return "linux-loong64"
	default:
		return ""
	}
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

// fetchRelease 查询仓库最新 Release。
func (m *Manager) fetchRelease(ctx context.Context, repo string) (*ghRelease, error) {
	rel, err := m.fetchGitHub(ctx, "/repos/"+repo+"/releases/latest")
	if err == nil {
		return rel, nil
	}
	var se *httpStatusError
	if !errors.As(err, &se) {
		return nil, err
	}
	switch se.code {
	case http.StatusNotFound:
		// releases/latest 对「没有任何已发布 Release」的公开仓库同样 404，与
		// 仓库不存在/私有从状态码上无法区分；补一次仓库元信息探测再定提示
		if m.repoVisible(ctx, repo) {
			return nil, fmt.Errorf("仓库 %s 可访问，但还没有发布任何 Release，无法检查更新（请先在 GitHub 上发布 Release）", repo)
		}
		return nil, fmt.Errorf("仓库 %s 不存在，或为私有仓库（需在设置-插件里配置 GitHub 访问令牌）", repo)
	case http.StatusUnauthorized:
		// 只有带了令牌 GitHub 才回 401：令牌填错或已失效
		return nil, fmt.Errorf("GitHub 访问令牌无效或已过期，请在设置-插件里检查令牌")
	}
	return nil, err
}

// repoVisible 探测仓库本身匿名是否可见（存在且公开）。
func (m *Manager) repoVisible(ctx context.Context, repo string) bool {
	_, err := m.fetchGitHub(ctx, "/repos/"+repo)
	return err == nil
}

// fetchReleaseByTag 查询仓库指定 tag 对应的 Release（tag 没发过 Release 时 404）。
func (m *Manager) fetchReleaseByTag(ctx context.Context, repo, tag string) (*ghRelease, error) {
	return m.fetchGitHub(ctx, "/repos/"+repo+"/releases/tags/"+tag)
}

// fetchGitHub 请求 GitHub API：先直连，失败时改走下载加速前缀重试。
// 配置了 GitHub 令牌（私有仓库）时随请求携带。
func (m *Manager) fetchGitHub(ctx context.Context, apiPath string) (*ghRelease, error) {
	token := ""
	if s, err := m.cfg.Get(); err == nil {
		token = s.GithubToken
	}
	rel, err := fetchReleaseVia(ctx, ghAPIBase+apiPath, token)
	if err == nil {
		return rel, nil
	}
	s, gerr := m.cfg.Get()
	if gerr == nil && s.DownloadProxy != "" {
		if rel2, err2 := fetchReleaseVia(ctx,
			strings.TrimSuffix(s.DownloadProxy, "/")+"/https://api.github.com"+apiPath, token); err2 == nil {
			return rel2, nil
		}
	}
	return nil, err
}

func fetchReleaseVia(ctx context.Context, url, token string) (*ghRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clashv")
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// 直连场景不走系统代理，加速前缀地址本身是可达端点
	hc := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("访问 GitHub 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, &httpStatusError{code: resp.StatusCode}
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func (r *ghRelease) findAsset(re *regexp.Regexp) (*ghAsset, bool) {
	for i, a := range r.Assets {
		if re.MatchString(a.Name) {
			return &r.Assets[i], true
		}
	}
	return nil, false
}

// ---- 升级进度 ----

// UpgradeProgress 一次升级任务的实时进度（内核/插件共用）。
type UpgradeProgress struct {
	Kind       string  `json:"kind"`       // core / plugin
	Stage      string  `json:"stage"`      // download / install / done / error
	Message    string  `json:"message"`    // 展示用文本（error 时为失败原因）
	Downloaded int64   `json:"downloaded"` // 已下载字节
	Total      int64   `json:"total"`      // 总字节（服务器未给出时为 0）
	Percent    float64 `json:"percent"`    // 0-100，未知总量时为 0
	Active     bool    `json:"active"`     // 任务是否进行中
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
// token 非空时随请求携带（私有仓库资产），跨主机重定向由 net/http 自动剥离。
func (m *Manager) downloadFile(ctx context.Context, candidates []string, dst, token string) error {
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
		err := downloadOnce(ctx, url, tmp, token, m.setProgress)
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
func downloadOnce(ctx context.Context, url, tmp, token string, onProg func(downloaded, total int64)) error {
	offset := int64(0)
	if st, err := os.Stat(tmp); err == nil {
		offset = st.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "clashv")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// assets API 端点要靠 Accept 拿到文件内容（否则返回 JSON 元数据）；
	// GitHub 应答 302 到预签名 S3，跨主机重定向时 Authorization 会被自动剥离
	if strings.HasPrefix(url, ghAPIBase) {
		req.Header.Set("Accept", "application/octet-stream")
	}
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
	st := CoreStatus{Path: path, Platform: platformName()}
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
	rel, err := m.fetchRelease(ctx, coreRepo)
	if err != nil {
		return "", err
	}
	plat := platformName()
	if plat == "" {
		return "", fmt.Errorf("无法识别当前设备架构（%s/%s），不支持自动下载内核", runtime.GOOS, runtime.GOARCH)
	}
	re, err := regexp.Compile(`^mihomo-` + regexp.QuoteMeta(plat) + `-v[\w.\-]+\.gz$`)
	if err != nil {
		return "", err
	}
	asset, ok := rel.findAsset(re)
	if !ok {
		return "", fmt.Errorf("最新版本 %s 没有 %s 平台的内核文件", rel.TagName, plat)
	}
	slog.Info("开始下载内核", "version", rel.TagName, "platform", plat, "url", asset.BrowserDownloadURL)
	if err := m.beginUpgrade("core", "准备下载 "+rel.TagName); err != nil {
		return "", err
	}

	// 先下载（gz 原始文件，可断点续传），成功后再停内核做替换，把停机窗口压到最小
	// mihomo 仓库公开，不需要令牌，加速前缀照常生效
	gzPath := m.cfg.CorePath() + ".download.gz"
	if err := m.downloadFile(ctx, m.downloadCandidates(asset.BrowserDownloadURL), gzPath, ""); err != nil {
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

// Release 里同时有 ipk/apk 各 8 个包：通用版 + 7 个架构精简版，全部
// PKGARCH:=all、靠包名区分架构，互相 CONFLICTS 只能装一个。
var pluginPkgNames = []string{
	"luci-app-clashv",
	"luci-app-clashv-arm64", "luci-app-clashv-armv7", "luci-app-clashv-mips",
	"luci-app-clashv-mipsle", "luci-app-clashv-amd64",
	"luci-app-clashv-riscv64", "luci-app-clashv-loong64",
}

// PluginPkg 为当前设备解析出的插件升级安装包。
type PluginPkg struct {
	Name   string `json:"name"`   // 包名，决定匹配 Release 里的哪个文件
	Format string `json:"format"` // ipk（opkg 系）或 apk（25.12+）
	Arch   string `json:"arch"`   // 设备 DISTRIB_ARCH，读不到为 all
}

// openwrtPkgFormat 按包管理器决定下载 ipk 还是 apk：apk 系（25.12+）优先，
// 否则 opkg 系；两者都没有返回空串。
func openwrtPkgFormat() string {
	for _, b := range []string{"apkg", "apk"} {
		if _, err := exec.LookPath(b); err == nil {
			return "apk"
		}
	}
	if _, err := exec.LookPath("opkg"); err == nil {
		return "ipk"
	}
	return ""
}

// distribArch 读 /etc/openwrt_release 里的设备架构（如 aarch64_cortex-a53）。
func distribArch() string {
	data, err := os.ReadFile("/etc/openwrt_release")
	if err != nil {
		return ""
	}
	m := regexp.MustCompile(`DISTRIB_ARCH=["']?([\w.+-]+)`)
	if mm := m.FindSubmatch(data); mm != nil {
		return string(mm[1])
	}
	return ""
}

// openwrtPkgArch 把 DISTRIB_ARCH 映射到架构精简包名后缀，与包安装脚本
// postinst 的 KEEP 映射保持一致；映射不上返回空串（退回通用版）。
func openwrtPkgArch(arch string) string {
	switch {
	case arch == "":
		return ""
	case strings.HasPrefix(arch, "aarch64"), arch == "arm64":
		return "arm64"
	case strings.HasPrefix(arch, "arm"):
		return "armv7"
	case strings.HasPrefix(arch, "mips64"):
		return "" // 64 位 MIPS 没有对应 Go 构建，退通用版（实际同样跑不了）
	case strings.HasPrefix(arch, "mipsel"):
		return "mipsle"
	case strings.HasPrefix(arch, "mips"):
		return "mips"
	case arch == "x86_64", arch == "amd64":
		return "amd64"
	case arch == "riscv64":
		return "riscv64"
	case arch == "loongarch64":
		return "loong64"
	}
	return ""
}

// installedPluginPkg 查当前已安装的 clashv 包名。升级优先原地替换同一个包：
// 通用版与各精简版互为 CONFLICTS，装错变体会被包管理器直接拒绝。
func installedPluginPkg(format string) string {
	if format == "apk" {
		// apk 数据库每包一段，包名记在 P: 行；路径按 apk-tools 版本两处都试
		for _, p := range []string{"/usr/lib/apk/db/installed", "/lib/apk/db/installed"} {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			for _, ln := range strings.Split(string(data), "\n") {
				if name, ok := strings.CutPrefix(ln, "P:"); ok {
					for _, want := range pluginPkgNames {
						if name == want {
							return want
						}
					}
				}
			}
		}
		bin := "apk"
		if _, err := exec.LookPath("apkg"); err == nil {
			bin = "apkg"
		}
		for _, name := range pluginPkgNames {
			// info -e：<包>已安装时退出码为 0；旧版 apk 不认识该参数也不致错装
			if err := exec.Command(bin, "info", "-e", name).Run(); err == nil {
				return name
			}
		}
		return ""
	}
	out, err := exec.Command("opkg", "list-installed").Output()
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(out), "\n") {
		f := strings.Fields(ln)
		for _, want := range pluginPkgNames {
			if len(f) >= 2 && f[0] == want {
				return want
			}
		}
	}
	return ""
}

// PluginPackagePlan 为当前设备选定升级包（纯本地探测，不访问网络）；
// 非 OpenWrt 环境返回 nil。
func (m *Manager) PluginPackagePlan() *PluginPkg {
	format := openwrtPkgFormat()
	if format == "" {
		return nil
	}
	arch := distribArch()
	name := installedPluginPkg(format)
	if name == "" {
		if short := openwrtPkgArch(arch); short != "" {
			name = "luci-app-clashv-" + short
		} else {
			name = "luci-app-clashv"
		}
	}
	if arch == "" {
		arch = "all"
	}
	return &PluginPkg{Name: name, Format: format, Arch: arch}
}

// pluginAssetRe 构造 Release 资产文件名的匹配规则：包名后紧跟数字开头的版本号
// （分隔符 ipk 用 _、apk 可能用 -），末段可选 _架构.扩展名。通用版的规则不会
// 误吃架构精简版——精简版包名在通用版名后多出 "-<arch>"，版本段不再是数字开头。
func pluginAssetRe(name, format string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `[._-][0-9][\w.+-]*(_[\w.+-]+)?\.` + format + `$`)
}

// pluginDownloadCandidates 返回插件包的下载候选。配置了 GitHub 令牌（私有
// 仓库）时走 assets API 端点直连——加速前缀的代理服务器不带用户凭据，请求
// 私有仓库必然 404，不能作为候选；browser_download_url 带令牌请求会 302 到
// 预签名 S3，作为 API 端点异常时的兜底。公开仓库照旧加速前缀优先。
func (m *Manager) pluginDownloadCandidates(asset *ghAsset, token string) []string {
	if token != "" {
		var urls []string
		if asset.URL != "" {
			urls = append(urls, asset.URL)
		}
		return append(urls, asset.BrowserDownloadURL)
	}
	return m.downloadCandidates(asset.BrowserDownloadURL)
}

// PreparePluginUpgrade 查询在线仓库最新 Release，匹配当前设备的 ipk/apk 并
// 下载到临时目录；安装由 InstallDownloadedPlugin 在响应返回后的后台执行。
func (m *Manager) PreparePluginUpgrade(ctx context.Context) (ver string, pkg PluginPkg, file string, err error) {
	plan := m.PluginPackagePlan()
	if plan == nil {
		return "", pkg, "", fmt.Errorf("未识别 OpenWrt 包管理器（opkg/apk），无法在线更新")
	}
	s, gerr := m.cfg.Get()
	if gerr != nil {
		return "", pkg, "", gerr
	}
	rel, gerr := m.fetchRelease(ctx, s.PluginRepo)
	if gerr != nil {
		return "", pkg, "", gerr
	}
	asset, ok := rel.findAsset(pluginAssetRe(plan.Name, plan.Format))
	if !ok {
		return "", pkg, "", fmt.Errorf("最新版本 %s 没有 %s 的 %s 包", rel.TagName, plan.Name, plan.Format)
	}
	if gerr = m.beginUpgrade("plugin", "准备下载 "+rel.TagName); gerr != nil {
		return "", pkg, "", gerr
	}
	file = filepath.Join(os.TempDir(), asset.Name)
	slog.Info("开始下载插件更新包", "version", rel.TagName, "pkg", plan.Name, "asset", asset.Name, "with_token", s.GithubToken != "")
	if gerr = m.downloadFile(ctx, m.pluginDownloadCandidates(asset, s.GithubToken), file, s.GithubToken); gerr != nil {
		m.finishProgress("error", "下载失败: "+gerr.Error())
		os.Remove(file)
		return "", pkg, "", gerr
	}
	m.finishProgress("install", "下载完成，安装中…")
	return rel.TagName, *plan, file, nil
}

// InstallDownloadedPlugin 用系统包管理器安装下载好的包，装完的 postinst 会
// 自动重启服务。服务被杀前 HTTP 响应必须已经发出，所以只能在后台 goroutine 跑。
func (m *Manager) InstallDownloadedPlugin(pkg PluginPkg, file string) error {
	var cmd *exec.Cmd
	if pkg.Format == "apk" {
		bin := "apk"
		if _, err := exec.LookPath("apkg"); err == nil {
			bin = "apkg"
		}
		cmd = exec.Command(bin, "add", "--allow-untrusted", file) // 自建包未签名，须跳过签名校验
	} else {
		cmd = exec.Command("opkg", "install", file)
	}
	out, err := cmd.CombinedOutput()
	os.Remove(file)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		slog.Warn("插件包安装失败", "pkg", pkg.Name, "out", msg)
		m.finishProgress("error", "安装失败: "+msg)
		return fmt.Errorf("安装失败: %s", msg)
	}
	slog.Info("插件包安装完成，postinst 将重启服务", "pkg", pkg.Name)
	m.finishProgress("done", "已安装 "+pkg.Name)
	return nil
}

// LatestPluginRelease 查询插件仓库最新 Release（含 ID，供新旧比较）。
func (m *Manager) LatestPluginRelease(ctx context.Context) (*ghRelease, error) {
	s, err := m.cfg.Get()
	if err != nil {
		return nil, err
	}
	return m.fetchRelease(ctx, s.PluginRepo)
}

// InstalledPluginReleaseID 查本地版本对应 Release 的数字 ID；查不到（自编译
// 版本、老 tag 没发过 Release）返回 0，调用方退回 tag 字符串比较。
func (m *Manager) InstalledPluginReleaseID(ctx context.Context, ver string) int64 {
	s, err := m.cfg.Get()
	if err != nil {
		return 0
	}
	rel, err := m.fetchReleaseByTag(ctx, s.PluginRepo, "v"+strings.TrimPrefix(ver, "v"))
	if err != nil {
		return 0
	}
	return rel.ID
}

// UpgradePlugin 非 OpenWrt 环境的兜底自更新：下载新版本裸二进制并替换自身，
// 替换后需重启服务生效。Release 里没有裸二进制资产，OpenWrt 一律走包流程。
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
	asset, ok := rel.findAsset(re)
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
	if err := m.downloadFile(ctx, m.pluginDownloadCandidates(asset, s.GithubToken), self+".download", s.GithubToken); err != nil {
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
