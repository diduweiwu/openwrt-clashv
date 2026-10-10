package core

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mihomo -v 输出版本提取：正式版取 vX.Y.Z，Alpha 取 alpha-<commit>，
// 都没有时退回首行原文。
func TestCoreVersionFromOutput(t *testing.T) {
	cases := map[string]string{
		"Mihomo Meta v1.19.2 linux arm64 with go1.23.1\n": "v1.19.2",
		"Mihomo Meta alpha-e4dd968 linux amd64 with go1.23\n": "alpha-e4dd968",
		"Mihomo Meta v1.19.2 linux arm64 with go1.23.1 darwin": "v1.19.2",
		"some random output\nsecond line":                      "some random output",
	}
	for in, want := range cases {
		if got := coreVersionFromOutput(in); got != want {
			t.Errorf("coreVersionFromOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

// gzip 魔数识别：.gz 内容为真、原始二进制为假、文件不存在为假。
func TestIsGzipFile(t *testing.T) {
	dir := t.TempDir()
	gzPath := filepath.Join(dir, "a.gz")
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte("mihomo"))
	zw.Close()
	os.WriteFile(gzPath, buf.Bytes(), 0o644)

	rawPath := filepath.Join(dir, "raw")
	os.WriteFile(rawPath, []byte("\x7fELF fake binary"), 0o644)

	if !isGzipFile(gzPath) {
		t.Error("isGzipFile(gz) = false, want true")
	}
	if isGzipFile(rawPath) {
		t.Error("isGzipFile(raw) = true, want false")
	}
	if isGzipFile(filepath.Join(dir, "missing")) {
		t.Error("isGzipFile(missing) = true, want false")
	}
}

// 试运行校验：可执行且输出含 mihomo 才放行，脚本输出 mihomo 版本行时提取版本号。
// 用 shell 脚本充当假内核（chmod +x 后 exec 语义与真内核一致）。
func TestProbeCore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell 脚本假内核仅适用于 unix")
	}
	dir := t.TempDir()
	ok := filepath.Join(dir, "mihomo")
	os.WriteFile(ok, []byte("#!/bin/sh\necho \"Mihomo Meta v1.19.2 linux arm64 with go1.23.1\"\n"), 0o755)
	ver, err := probeCore(ok)
	if err != nil {
		t.Fatalf("probeCore(ok) err = %v, want nil", err)
	}
	if ver != "v1.19.2" {
		t.Errorf("probeCore(ok) = %q, want v1.19.2", ver)
	}

	bad := filepath.Join(dir, "notmihomo")
	os.WriteFile(bad, []byte("#!/bin/sh\necho \"hello world\"\n"), 0o755)
	if _, err := probeCore(bad); err == nil || !strings.Contains(err.Error(), "mihomo") {
		t.Errorf("probeCore(bad) err = %v, want 非 mihomo 输出报错", err)
	}

	noexec := filepath.Join(dir, "noexec")
	os.WriteFile(noexec, []byte("plain text"), 0o644)
	if _, err := probeCore(noexec); err == nil {
		t.Error("probeCore(noexec) err = nil, want 不可执行报错")
	}
}

// 上传落盘：正常写入、空内容拒绝、超限拒绝且不留残留文件。
func TestWriteUpload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "mihomo.upload")

	n, err := writeUpload(path, bytes.NewReader([]byte("hello core")))
	if err != nil || n != 10 {
		t.Fatalf("writeUpload(ok) = %d, %v; want 10, nil", n, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello core" {
		t.Errorf("落盘内容 = %q, want hello core", data)
	}

	if _, err := writeUpload(path, bytes.NewReader(nil)); err == nil {
		t.Error("writeUpload(empty) err = nil, want 空内容报错")
	}

	if _, err := writeUpload(path, bytes.NewReader(make([]byte, maxAssetSize+1))); err == nil {
		t.Error("writeUpload(oversize) err = nil, want 超限报错")
	}
	if _, serr := os.Stat(path); serr == nil {
		t.Error("失败后残留文件应被清理")
	}
}
