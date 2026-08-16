package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func cmdSetup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	gateDir := fs.String("gate-dir", "", "検収ゲートの作業ディレクトリ (必須)")
	projectDir := fs.String("project-dir", "", "検収の対象ディレクトリ (既定: カレントディレクトリ)")
	mode := fs.String("mode", "", "mechanical または claude (必須)")
	checkCmd := fs.String("check-cmd", "", "mode=mechanical のとき、終了コードで合否を判定するコマンド")
	promptFile := fs.String("prompt-file", "", "mode=claude のとき、検収プロンプトのファイルパス")
	achievementDir := fs.String("achievement-dir", "achievement", "mode=claude のとき、成果報告を置くディレクトリ (project-dir基準の相対パス、または絶対パス)")
	quietSeconds := fs.Int("quiet-seconds", 20, "ファイル変更が止んでから検収するまでの静穏時間 (秒)")
	pollInterval := fs.Int("poll-interval", 10, "静穏かどうかを見にいく間隔 (秒、quiet-secondsより小さくすること)")
	maxInterval := fs.Int("max-interval", 180, "静穏にならなくても検収を強制する上限間隔 (秒)")
	checkTimeout := fs.Int("check-timeout", 600, "検収コマンド/判定1回あたりのタイムアウト (秒)")
	retention := fs.Int("retention", 20, "保持する検収結果の世代数")
	maxBudgetUSD := fs.String("max-budget-usd", "0.50", "mode=claude のとき、検収1回あたりの上限予算 (USD)")
	judgeModel := fs.String("model", "claude-haiku-4-5-20251001", "mode=claude のとき、判定に使うモデル")
	judgeTools := fs.String("judge-tools", "Read,Grep,Glob,Bash", "mode=claude のとき、判定側に許可するツール")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *gateDir == "" {
		fmt.Fprintln(os.Stderr, "error: --gate-dir is required")
		return 2
	}
	if *mode != "mechanical" && *mode != "claude" {
		fmt.Fprintln(os.Stderr, "error: --mode must be mechanical or claude")
		return 2
	}
	if *projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		*projectDir = wd
	}
	if fi, err := os.Stat(*projectDir); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "error: --project-dir does not exist: %s\n", *projectDir)
		return 2
	}
	if *pollInterval >= *quietSeconds {
		fmt.Fprintln(os.Stderr, "error: --poll-interval must be smaller than --quiet-seconds (静穏判定の粒度が粗すぎます)")
		return 2
	}
	if *quietSeconds >= *maxInterval {
		fmt.Fprintln(os.Stderr, "error: --quiet-seconds must be smaller than --max-interval (フェイルセーフが機能しません)")
		return 2
	}
	if *retention < 1 {
		fmt.Fprintln(os.Stderr, "error: --retention must be >= 1")
		return 2
	}
	if *checkTimeout < 1 {
		fmt.Fprintln(os.Stderr, "error: --check-timeout must be >= 1")
		return 2
	}

	absProjectDir, err := filepath.Abs(*projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	resolvedPromptFile := ""
	resolvedAchievementDir := ""
	if *mode == "claude" {
		if *promptFile == "" {
			fmt.Fprintln(os.Stderr, "error: --prompt-file is required for mode=claude")
			return 2
		}
		abs, err := filepath.Abs(*promptFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
		if !fileExists(abs) {
			fmt.Fprintf(os.Stderr, "error: --prompt-file must point to an existing file: %s\n", *promptFile)
			return 2
		}
		resolvedPromptFile = abs
		if _, err := exec.LookPath("claude"); err != nil {
			fmt.Fprintln(os.Stderr, "error: claude CLI not found in PATH (required for mode=claude)")
			return 2
		}
		// achievement-dir は project-dir 基準の相対パスとして解決する (絶対パスなら
		// そのまま使う)。ゲートはここを監視し、検収結果の鮮度もここを基準に判定する。
		if filepath.IsAbs(*achievementDir) {
			resolvedAchievementDir = filepath.Clean(*achievementDir)
		} else {
			resolvedAchievementDir = filepath.Join(absProjectDir, *achievementDir)
		}
		if err := os.MkdirAll(resolvedAchievementDir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "error: failed to create achievement-dir:", err)
			return 1
		}
	} else {
		if *checkCmd == "" {
			fmt.Fprintln(os.Stderr, "error: --check-cmd is required for mode=mechanical")
			return 2
		}
	}

	if pidBytes, err := os.ReadFile(filepath.Join(*gateDir, "gate.pid")); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes))); err == nil {
			if proc, err := os.FindProcess(pid); err == nil && proc.Signal(syscall.Signal(0)) == nil {
				fmt.Fprintf(os.Stderr, "error: a gate loop is already running (pid %d) in %s — run stop first\n", pid, *gateDir)
				return 2
			}
		}
	}

	if err := os.MkdirAll(filepath.Join(*gateDir, "results"), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	absGateDir, err := filepath.Abs(*gateDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	os.Remove(filepath.Join(absGateDir, "STOP"))
	os.Remove(filepath.Join(absGateDir, "CHECK_NOW"))
	os.Remove(filepath.Join(absGateDir, "stop-guard-blocks")) // Stopフックの連続ブロック回数 (前回分の残りを消す)

	// --- 鍵ペアの生成 (mode=claude のときだけ)。秘密鍵はこのプロセスのメモリ上
	//     にだけ存在させ、ディスクには一切書かない。 ---
	haveKey := false
	var seedHex string
	if *mode == "claude" {
		pub, priv, err := generateKeyPair()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: failed to generate signing key:", err)
			return 1
		}
		pemBytes, err := publicKeyToPEM(pub)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: failed to encode public key:", err)
			return 1
		}
		if err := os.WriteFile(filepath.Join(absGateDir, "public_key.pem"), pemBytes, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "error: failed to write public key:", err)
			return 1
		}
		seedHex = hex.EncodeToString(priv.Seed())
		haveKey = true
	}

	cfg := &GateConfig{
		Mode:                *mode,
		ProjectDir:          absProjectDir,
		AchievementDir:      resolvedAchievementDir,
		CheckCmd:            *checkCmd,
		PromptFile:          resolvedPromptFile,
		QuietSeconds:        *quietSeconds,
		PollInterval:        *pollInterval,
		MaxInterval:         *maxInterval,
		CheckTimeoutSeconds: *checkTimeout,
		Retention:           *retention,
		MaxBudgetUSD:        *maxBudgetUSD,
		JudgeModel:          *judgeModel,
		JudgeTools:          *judgeTools,
	}
	if err := saveConfig(absGateDir, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "error: failed to write config:", err)
		return 1
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot resolve own executable path:", err)
		return 1
	}
	// gatectl自身の絶対パスをgate-dir配下に書いておく。作業担当のセッションは
	// スキル起動時に渡されるBase directoryを毎ターン覚えていられるとは限らない
	// (シェル変数はBashツール呼び出しをまたいで残らず、長い会話ではcompactionも
	// 挟まる)。.gate/gatectl-pathを読めば、以後はBase directoryを介さずに
	// gatectl自身を呼び直せる。
	if err := os.WriteFile(filepath.Join(absGateDir, "gatectl-path"), []byte(self+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "error: failed to write gatectl-path:", err)
		return 1
	}

	logFile, err := os.OpenFile(filepath.Join(absGateDir, "gate.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: failed to open gate.log:", err)
		return 1
	}
	defer logFile.Close()

	// setsid で新しいセッションに切り離す (nohup + disown 相当)。作業担当の
	// claude code とは独立したプロセスとして、以後は自分のタイミングで動く。
	cmd := exec.Command(self, "run", absGateDir)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = os.Environ()
	if haveKey {
		// コマンドライン引数には渡さない (ps や /proc/<pid>/cmdline に載らないようにするため)。
		// 環境変数はこの子プロセスの環境にだけ継承させる。
		cmd.Env = append(cmd.Env, "GATE_PRIVATE_KEY_SEED="+seedHex)
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "error: failed to start gate loop:", err)
		return 1
	}
	seedHex = "" // ベストエフォートでこのプロセスの変数からも消す

	pid := cmd.Process.Pid
	if err := os.WriteFile(filepath.Join(absGateDir, "gate.pid"), []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "error: failed to write pid file:", err)
		return 1
	}
	// cmd.Wait は呼ばない。setup プロセスはこのあとすぐ終了し、run は孤児化して
	// init に再親化される (ゾンビにはならない、標準的なデーモン化パターン)。

	time.Sleep(300 * time.Millisecond)
	if proc, err := os.FindProcess(pid); err != nil || proc.Signal(syscall.Signal(0)) != nil {
		fmt.Fprintf(os.Stderr, "error: gate loop failed to start — see %s\n", filepath.Join(absGateDir, "gate.log"))
		return 1
	}

	fmt.Println("検収ゲートを起動しました")
	fmt.Printf("  pid          : %d\n", pid)
	fmt.Printf("  gate-dir     : %s\n", absGateDir)
	fmt.Printf("  project-dir  : %s\n", absProjectDir)
	fmt.Printf("  mode         : %s\n", *mode)
	fmt.Printf("  timing       : quiet=%ds poll=%ds max=%ds\n", *quietSeconds, *pollInterval, *maxInterval)
	if haveKey {
		fmt.Printf("  achievement  : %s\n", resolvedAchievementDir)
		fmt.Printf("  public key   : %s\n", filepath.Join(absGateDir, "public_key.pem"))
		fmt.Println("  signing      : ed25519 (すべての結果に署名します)")
	} else {
		fmt.Println("  signing      : なし (mechanicalモードは再実行で確認できるため署名しません)")
	}
	fmt.Printf("  results      : %s\n", filepath.Join(absGateDir, "results"))
	fmt.Printf("  stop with    : %s stop --gate-dir %s\n", self, absGateDir)
	return 0
}
