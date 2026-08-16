package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// cmdStop は検収ループを止める。STOPファイルを置いたうえで、プロセスにも
// 直接シグナルを送る。ループは poll-interval ごとにSTOPを見にいくので、
// 静穏判定を待っている間なら数秒〜poll-interval秒で気づいて自分から終了する。
// 検収コマンド実行中や sleep 中で気づくのが遅い場合は、SIGTERM、それでも
// 止まらなければ SIGKILL で確実に止める。
func cmdStop(args []string) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	gateDir := fs.String("gate-dir", "", "検収ゲートの作業ディレクトリ (必須)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *gateDir == "" {
		fmt.Fprintln(os.Stderr, "error: --gate-dir is required")
		return 2
	}

	os.WriteFile(filepath.Join(*gateDir, "STOP"), []byte{}, 0o644)

	pidBytes, err := os.ReadFile(filepath.Join(*gateDir, "gate.pid"))
	if err != nil {
		fmt.Println("gate.pidが見つかりません。すでに停止しているか、起動していません。")
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		fmt.Println("gate.pidの内容が不正です。")
		return 1
	}

	proc, err := os.FindProcess(pid)
	if err != nil || proc.Signal(syscall.Signal(0)) != nil {
		fmt.Println("検収ループはすでに停止していました。")
		return 0
	}

	proc.Signal(syscall.SIGTERM)
	time.Sleep(1 * time.Second)
	if proc.Signal(syscall.Signal(0)) == nil {
		proc.Signal(syscall.SIGKILL)
	}
	fmt.Printf("検収ループ (pid %d) を止めました。\n", pid)
	return 0
}
