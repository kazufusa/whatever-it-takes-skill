package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// cmdRun は検収ループ本体。setup がバックグラウンドで起動し、以後は作業担当の
// claude code とは独立に動き続ける。直接ではなく setup 経由で起動すること。
//
// 検収のタイミングは固定間隔のタイマーにしない。監視対象 (watchDir、後述) が
// quiet-seconds のあいだ変化しなくなった時点で検収する。作業の途中、ファイルが
// 壊れた中間状態にあるときに検収してしまうのを避けるため。動き続けて静穏に
// ならない場合に備えて max-interval を上限のフェイルセーフとして使う。
// 作業担当が CHECK_NOW を置けば、静穏判定を待たずに次のポーリングで検収する。
//
// 監視対象は mode によって違う。claudeモードでは achievement-dir (成果報告の
// 置き場) だけを見る。作業担当はここに成果報告を配置・入れ替えるので、静穏判定
// が「報告の更新が落ち着いた」という意味を持つ。mechanicalモードには成果報告の
// 概念が無く、check-cmdが直接プロジェクトを検査するので project-dir 全体を見る。
func cmdRun(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gatectl run GATE_DIR")
		return 2
	}
	gateDir, err := filepath.Abs(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	cfg, err := loadConfig(gateDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error loading config:", err)
		return 1
	}

	var priv ed25519.PrivateKey
	if cfg.Mode == "claude" {
		seedHex := os.Getenv("GATE_PRIVATE_KEY_SEED")
		if seedHex == "" {
			fmt.Fprintln(os.Stderr, "error: GATE_PRIVATE_KEY_SEED is not set (required for mode=claude)")
			return 1
		}
		p, err := privateKeyFromHexSeed(seedHex)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: invalid GATE_PRIVATE_KEY_SEED:", err)
			return 1
		}
		priv = p
	}

	resultsDir := filepath.Join(gateDir, "results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	if err := os.Chdir(cfg.ProjectDir); err != nil {
		logLine(gateDir, fmt.Sprintf("error: project-dir not found: %s", cfg.ProjectDir))
		return 1
	}

	watch := watchTarget(cfg)

	activityMarker := filepath.Join(gateDir, ".activity_marker")
	touchFile(activityMarker)
	lastActivity := time.Now()
	lastCheck := time.Now()

	logLine(gateDir, fmt.Sprintf("gate loop started (mode=%s quiet=%ds poll=%ds max=%ds watch=%s pid=%d)",
		cfg.Mode, cfg.QuietSeconds, cfg.PollInterval, cfg.MaxInterval, watch, os.Getpid()))

	stopFile := filepath.Join(gateDir, "STOP")
	checkNowFile := filepath.Join(gateDir, "CHECK_NOW")

	for {
		if fileExists(stopFile) {
			logLine(gateDir, "STOP file detected, exiting")
			return 0
		}

		time.Sleep(time.Duration(cfg.PollInterval) * time.Second)
		now := time.Now()

		changed, scanErr := hasActivitySince(watch, gateDir, activityMarker)
		if scanErr != nil {
			logLine(gateDir, fmt.Sprintf("warning: activity scan failed: %v", scanErr))
		}
		if changed {
			touchFile(activityMarker)
			lastActivity = now
			continue
		}

		quietFor := now.Sub(lastActivity)
		sinceLastCheck := now.Sub(lastCheck)

		trigger := ""
		switch {
		case fileExists(checkNowFile):
			os.Remove(checkNowFile)
			trigger = "requested"
		case quietFor >= time.Duration(cfg.QuietSeconds)*time.Second:
			trigger = "quiet"
		case sinceLastCheck >= time.Duration(cfg.MaxInterval)*time.Second:
			trigger = "max-interval"
		}
		if trigger == "" {
			continue
		}

		verdict, reason := runCheck(gateDir, cfg)
		ts := time.Now().UTC().Format("20060102T150405Z")
		resultFile := filepath.Join(resultsDir, fmt.Sprintf("result-%s.json", ts))

		result := map[string]interface{}{
			"timestamp": ts,
			"verdict":   verdict,
			"reason":    reason,
			"mode":      cfg.Mode,
			"trigger":   trigger,
			"signed":    cfg.Mode == "claude",
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		if err := os.WriteFile(resultFile, b, 0o644); err != nil {
			logLine(gateDir, fmt.Sprintf("warning: failed to write result file: %v", err))
		}

		if cfg.Mode == "claude" {
			sig := signData(priv, b)
			if err := os.WriteFile(resultFile+".sig", sig, 0o644); err != nil {
				logLine(gateDir, fmt.Sprintf("warning: failed to sign %s: %v", resultFile, err))
			}
		}

		logLine(gateDir, fmt.Sprintf("check verdict=%s trigger=%s", verdict, trigger))
		pruneOldResults(resultsDir, cfg.Retention)
		lastCheck = time.Now()

		if verdict == "ok" {
			os.WriteFile(filepath.Join(gateDir, "OK"), []byte(resultFile+"\n"), 0o644)
			logLine(gateDir, "verdict=ok, gate satisfied, exiting")
			return 0
		}
	}
}

func runCheck(gateDir string, cfg *GateConfig) (verdict, reason string) {
	if cfg.Mode == "mechanical" {
		return runMechanicalCheck(cfg)
	}
	return runClaudeCheck(gateDir, cfg)
}

func runMechanicalCheck(cfg *GateConfig) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.CheckTimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", cfg.CheckCmd)
	out, err := cmd.CombinedOutput()
	verdict := "ok"
	if err != nil {
		verdict = "not_ok"
	}
	reason := string(out)
	if len(reason) > 4000 {
		reason = reason[len(reason)-4000:]
	}
	return verdict, reason
}

func runClaudeCheck(gateDir string, cfg *GateConfig) (string, string) {
	promptBytes, err := os.ReadFile(cfg.PromptFile)
	if err != nil {
		return "not_ok", fmt.Sprintf("failed to read prompt file: %v", err)
	}

	const schema = `{"type":"object","properties":{"verdict":{"type":"string","enum":["ok","not_ok"]},"reason":{"type":"string"}},"required":["verdict","reason"],"additionalProperties":false}`

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.CheckTimeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude",
		"-p", string(promptBytes),
		"--model", cfg.JudgeModel,
		"--no-session-persistence",
		"--permission-mode", "bypassPermissions",
		"--tools", cfg.JudgeTools,
		"--max-budget-usd", cfg.MaxBudgetUSD,
		"--output-format", "json",
		"--json-schema", schema,
	)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	out, err := cmd.Output()

	stderrPath := filepath.Join(gateDir, "last-judge-stderr.log")
	os.WriteFile(stderrPath, stderrBuf.Bytes(), 0o644)

	if err != nil {
		return "not_ok", fmt.Sprintf("judge invocation failed: %v — see %s", err, stderrPath)
	}

	var envelope struct {
		StructuredOutput struct {
			Verdict string `json:"verdict"`
			Reason  string `json:"reason"`
		} `json:"structured_output"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		snippet := string(out)
		if len(snippet) > 2000 {
			snippet = snippet[:2000]
		}
		return "not_ok", fmt.Sprintf("judge did not return valid structured output: %s", snippet)
	}
	v := envelope.StructuredOutput.Verdict
	if v != "ok" && v != "not_ok" {
		return "not_ok", fmt.Sprintf("judge returned an unexpected verdict value: %q", v)
	}
	return v, envelope.StructuredOutput.Reason
}

// watchTarget は、静穏検知と鮮度チェックの対象ディレクトリを返す。claudeモード
// ではachievement-dir (無ければproject-dirにフォールバック)、mechanicalモード
// ではproject-dirを見る。
func watchTarget(cfg *GateConfig) string {
	if cfg.Mode == "claude" && cfg.AchievementDir != "" {
		return cfg.AchievementDir
	}
	return cfg.ProjectDir
}

// hasActivitySince は、markerPath より新しいファイルが watchDir 配下にあるかを見る。
// .git と gateDir は除外する。最初の1件が見つかった時点で走査を打ち切るので、
// 対象が大きくても軽い。
func hasActivitySince(watchDir, gateDir, markerPath string) (bool, error) {
	markerInfo, err := os.Stat(markerPath)
	if err != nil {
		return true, err // マーカーが読めなければ安全側 (=変化あり扱い) に倒す
	}
	markerTime := markerInfo.ModTime()

	absGateDir, err := filepath.Abs(gateDir)
	if err != nil {
		absGateDir = gateDir
	}

	found := false
	walkErr := filepath.WalkDir(watchDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 読めないパスは無視して続行
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			if absPath, aerr := filepath.Abs(path); aerr == nil && absPath == absGateDir {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(markerTime) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found, walkErr
}

func pruneOldResults(resultsDir string, retention int) {
	entries, err := os.ReadDir(resultsDir)
	if err != nil {
		return
	}
	var jsonFiles []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "result-") && strings.HasSuffix(name, ".json") {
			jsonFiles = append(jsonFiles, name)
		}
	}
	// ファイル名にタイムスタンプが埋め込まれているので、文字列ソートがそのまま
	// 時系列になる。mtimeに頼らないので、ファイルシステムの時刻精度に左右されない。
	sort.Sort(sort.Reverse(sort.StringSlice(jsonFiles)))
	for i, name := range jsonFiles {
		if i < retention {
			continue
		}
		full := filepath.Join(resultsDir, name)
		os.Remove(full)
		os.Remove(full + ".sig")
	}
}

func touchFile(path string) {
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		if f, ferr := os.Create(path); ferr == nil {
			f.Close()
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func logLine(gateDir, msg string) {
	line := fmt.Sprintf("%s %s\n", time.Now().UTC().Format(time.RFC3339), msg)
	f, err := os.OpenFile(filepath.Join(gateDir, "gate.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(line)
}
