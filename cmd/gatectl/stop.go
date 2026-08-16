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

// cmdStop は検収ループを止める。プロセスに直接シグナルを送るだけ。
// SIGTERM、それでも止まらなければ SIGKILL で確実に止める。
//
// かつてはSTOPファイルも置き、ループがpoll-intervalごとにそれを見て自分から
// 終わるようになっていた。しかし run はSIGTERMのハンドラを持たない (デフォルト
// 動作である即終了のまま) ので、この直後に送るSIGTERMがほぼ確実に先に効く。
// ループが次のポーリングでSTOPファイルに気づく場面は実質発生しない、書くだけ
// で読まれない飾りだったので削除した。
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

	pidBytes, err := os.ReadFile(filepath.Join(internalDir(*gateDir), "gate.pid"))
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
