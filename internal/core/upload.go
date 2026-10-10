package core

// 手动上传内核：接收浏览器上传的内核文件（原始二进制或 mihomo Release 的
// .gz 压缩包），校验通过才替换当前渠道的内核文件，失败不动现有内核。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// writeUpload 把上传内容覆盖写到 path，父目录不存在时自动创建。
// 超过 maxAssetSize 判定过大、空内容判定无效，两种情况都会清掉已写内容。
// 返回写入字节数与错误。
//
// 示例：n, err := writeUpload(corePath+".upload", r.Body)
func writeUpload(path string, src io.Reader) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, io.LimitReader(src, maxAssetSize+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxAssetSize {
		err = fmt.Errorf("文件过大（上限 %s）", humanBytes(maxAssetSize))
	}
	if err == nil && n == 0 {
		err = errors.New("上传内容为空")
	}
	if err != nil {
		os.Remove(path)
		return n, err
	}
	return n, nil
}

// isGzipFile 按文件头魔数（0x1f 0x8b）判断是否 gzip 压缩包；读取失败视为否。
func isGzipFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [2]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return false
	}
	return head[0] == 0x1f && head[1] == 0x8b
}

// probeCore 试运行内核 -v 验证：文件可在当前设备执行（架构对得上，错架构
// 会得到 exec format error），且输出确实是 mihomo。返回展示用版本号。
//
// 示例：ver, err := probeCore("/tmp/mihomo.upload")
func probeCore(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-v").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("内核试运行失败: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("文件无法在当前设备运行（可能是其他架构的内核，或已损坏）")
	}
	first := strings.ToLower(strings.SplitN(string(out), "\n", 2)[0])
	if !strings.Contains(first, "mihomo") {
		return "", errors.New("试运行输出未识别为 mihomo 内核，已取消安装")
	}
	return coreVersionFromOutput(string(out)), nil
}

// coreVersionFromOutput 从 mihomo -v 输出提取展示版本号：正式版取 vX.Y.Z 段
//（如 "Mihomo Meta v1.19.2 linux arm64 with go1.23.1" → v1.19.2），
// Alpha 版没有 v 前缀版本号，取 alpha-<commit> 段；都没有时退回首行原文。
//
// 示例：v := coreVersionFromOutput("Mihomo Meta alpha-e4dd968 linux arm64\n…")
func coreVersionFromOutput(out string) string {
	line := strings.TrimSpace(strings.TrimPrefix(
		strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]), "Mihomo Meta"))
	for _, f := range strings.Fields(line) {
		if (strings.HasPrefix(f, "v") && strings.Contains(f, ".")) || strings.HasPrefix(f, "alpha-") {
			return f
		}
	}
	return line
}

// UploadCore 手动上传内核并安装到当前渠道（core_channel）。完整流程：
// 上传内容落盘临时文件 → 按文件头识别 .gz 并解压 → 加执行权限 → 试运行校验
//（能执行且输出为 mihomo）→ 全部通过才停内核、原子替换当前渠道的内核文件，
// 并按替换前的运行状态重启。任一步失败都只清理临时文件，现有内核不受影响。
//
// 安装到哪个渠道跟随当前设置（CorePath 已按渠道区分）；上传文件本身的版本/
// 渠道不做校验——用户有意把 Release 内核装进 Alpha 渠道时不拦。
//
// 与在线升级共用升级槽位（beginUpgrade）：上传期间不能同时发起在线升级，
// 反之亦然；进度可在 /api/upgrade/progress 看到。
//
// 参数 src 为上传的文件内容；返回从 -v 输出识别的版本号。内核替换后启动
// 失败时返回版本号与错误（与 UpgradeCore 行为一致）。
//
// 示例：ver, err := mgr.UploadCore(ctx, file)
func (m *Manager) UploadCore(ctx context.Context, src io.Reader) (string, error) {
	if err := m.beginUpgrade("core", "正在接收上传的内核…"); err != nil {
		return "", err
	}
	defer m.endUpgrade()

	target := m.cfg.CorePath()
	upload := target + ".upload"
	if _, err := writeUpload(upload, src); err != nil {
		m.finishProgress("error", "上传失败: "+err.Error())
		return "", err
	}
	if ctx.Err() != nil { // 上传途中被取消（升级取消接口或连接中断）
		os.Remove(upload)
		m.finishProgress("error", "已取消")
		return "", errors.New("已取消")
	}

	m.finishProgress("install", "校验上传的内核…")
	bin := upload
	if isGzipFile(upload) { // Release 下载页的 .gz 压缩包，先解压再校验
		if err := gunzipFile(upload, upload+".bin"); err != nil {
			os.Remove(upload)
			m.finishProgress("error", err.Error())
			return "", err
		}
		os.Remove(upload)
		bin = upload + ".bin"
	}
	if err := os.Chmod(bin, 0o755); err != nil {
		os.Remove(bin)
		m.finishProgress("error", err.Error())
		return "", err
	}
	version, err := probeCore(bin)
	if err != nil {
		os.Remove(bin)
		m.finishProgress("error", err.Error())
		return "", err
	}

	// 校验通过：与在线升级同流程，把停内核窗口压到替换一步
	wasRunning := m.Running()
	if wasRunning {
		if err := m.Stop(); err != nil {
			os.Remove(bin)
			m.finishProgress("error", "停止内核失败: "+err.Error())
			return "", err
		}
	}
	if err := os.Rename(bin, target); err != nil {
		os.Remove(bin)
		m.finishProgress("error", err.Error())
		if wasRunning {
			_ = m.Start()
		}
		return "", err
	}
	slog.Info("手动上传的内核已安装", "channel", m.CoreChannel(), "version", version, "path", target)
	m.finishProgress("done", "已安装 "+version)
	if wasRunning {
		if err := m.Start(); err != nil {
			return version, fmt.Errorf("内核已更新但启动失败: %w", err)
		}
	}
	return version, nil
}
