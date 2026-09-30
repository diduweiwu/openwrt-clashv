package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRotateLiveRotatesAndTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "core.log")
	body := strings.Repeat("x", 4096)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rotateLive(path, 1024)

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 0 {
		t.Fatalf("原文件应被截断为 0，实际 %d 字节", st.Size())
	}
	old, err := os.ReadFile(path + ".old")
	if err != nil {
		t.Fatal(err)
	}
	if string(old) != body {
		t.Fatalf("旧档内容不完整: %d 字节", len(old))
	}
}

func TestRotateLiveSkipsSmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "core.log")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	rotateLive(path, LogMaxBytes)
	if _, err := os.Stat(path + ".old"); !os.IsNotExist(err) {
		t.Fatalf("未超限不应产生旧档")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 5 {
		t.Fatalf("未超限原文件不应被截断，实际 %d 字节", st.Size())
	}
}

func TestCleanOldLogsRemovesExpiredOnly(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "core.log.old")
	fresh := filepath.Join(dir, "clashv.log.old")
	other := filepath.Join(dir, "core.log")
	for _, p := range []string{stale, fresh, other} {
		if err := os.WriteFile(p, []byte("log"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-oldLogRetention - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	cleanOldLogs(dir)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("超期旧档应被删除")
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s 不应被删除: %v", p, err)
		}
	}
}
