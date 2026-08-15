package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneOldResultsKeepsNewestAndDropsRest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"result-20260815T100000Z.json",
		"result-20260815T110000Z.json",
		"result-20260815T120000Z.json",
		"result-20260815T130000Z.json",
		"result-20260815T140000Z.json",
	}
	for _, n := range names {
		mustWrite(t, filepath.Join(dir, n), "{}")
		mustWrite(t, filepath.Join(dir, n+".sig"), "sig")
	}

	pruneOldResults(dir, 2)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 4 { // 新しい2件 x (.json + .sig)
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("expected 4 remaining files (2 results x .json+.sig), got %d: %v", len(entries), names)
	}

	mustExist(t, filepath.Join(dir, "result-20260815T140000Z.json"))
	mustExist(t, filepath.Join(dir, "result-20260815T140000Z.json.sig"))
	mustExist(t, filepath.Join(dir, "result-20260815T130000Z.json"))
	mustNotExist(t, filepath.Join(dir, "result-20260815T120000Z.json"))
	mustNotExist(t, filepath.Join(dir, "result-20260815T100000Z.json"))
	mustNotExist(t, filepath.Join(dir, "result-20260815T100000Z.json.sig"))
}

func TestPruneOldResultsNoOpWhenUnderRetention(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "result-20260815T100000Z.json"), "{}")

	pruneOldResults(dir, 20)

	mustExist(t, filepath.Join(dir, "result-20260815T100000Z.json"))
}

func TestHasActivitySinceDetectsNewFile(t *testing.T) {
	projectDir := t.TempDir()
	gateDir := t.TempDir()
	marker := filepath.Join(gateDir, ".activity_marker")
	mustWrite(t, marker, "")

	// マーカーより確実に新しくなるよう、少し時刻を進める。
	time.Sleep(20 * time.Millisecond)
	mustWrite(t, filepath.Join(projectDir, "changed.txt"), "x")

	changed, err := hasActivitySince(projectDir, gateDir, marker)
	if err != nil {
		t.Fatalf("hasActivitySince: %v", err)
	}
	if !changed {
		t.Fatal("expected activity to be detected after writing a new file")
	}
}

func TestHasActivitySinceIgnoresGitAndGateDir(t *testing.T) {
	projectDir := t.TempDir()
	gateDir := filepath.Join(projectDir, ".gate")
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(gateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	marker := filepath.Join(gateDir, ".activity_marker")
	mustWrite(t, marker, "")

	time.Sleep(20 * time.Millisecond)
	// .git と gate-dir 配下だけを変更する。project-dir 直下は変更しない。
	mustWrite(t, filepath.Join(projectDir, ".git", "FETCH_HEAD"), "x")
	mustWrite(t, filepath.Join(gateDir, "results-dummy.json"), "x")

	changed, err := hasActivitySince(projectDir, gateDir, marker)
	if err != nil {
		t.Fatalf("hasActivitySince: %v", err)
	}
	if changed {
		t.Fatal(".git and gate-dir changes should not count as project activity")
	}
}

func TestHasActivitySinceQuietWhenNothingChanged(t *testing.T) {
	projectDir := t.TempDir()
	gateDir := t.TempDir()
	mustWrite(t, filepath.Join(projectDir, "stable.txt"), "x")

	// マーカーをファイル作成よりあとに触るので、以後は「変化なし」のはず。
	time.Sleep(20 * time.Millisecond)
	marker := filepath.Join(gateDir, ".activity_marker")
	mustWrite(t, marker, "")

	changed, err := hasActivitySince(projectDir, gateDir, marker)
	if err != nil {
		t.Fatalf("hasActivitySince: %v", err)
	}
	if changed {
		t.Fatal("expected no activity when nothing changed after the marker")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if !fileExists(path) {
		t.Fatalf("expected %s to exist, but it does not", path)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if fileExists(path) {
		t.Fatalf("expected %s to be removed, but it still exists", path)
	}
}
