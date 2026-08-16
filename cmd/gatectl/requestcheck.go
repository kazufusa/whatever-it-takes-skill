package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// cmdRequestCheck は「今すぐ検収してほしい」を検収ループに伝える。静穏検知や
// 上限間隔を待たずに、次のポーリング (poll-interval以内) で検収を走らせる。
// 検収そのものを自分で実行するわけではない。いつ検収するかを早めるだけで、
// 何を検収するか・結果がどうなるかには関与しない。
func cmdRequestCheck(args []string) int {
	fs := flag.NewFlagSet("request-check", flag.ContinueOnError)
	gateDir := fs.String("gate-dir", "", "検収ゲートの作業ディレクトリ (必須)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *gateDir == "" {
		fmt.Fprintln(os.Stderr, "error: --gate-dir is required")
		return 2
	}
	if !fileExists(filepath.Join(internalDir(*gateDir), "gate.pid")) {
		fmt.Fprintf(os.Stderr, "error: no gate found at %s (has setup run?)\n", *gateDir)
		return 2
	}
	touchFile(filepath.Join(internalDir(*gateDir), "CHECK_NOW"))
	fmt.Println("検収リクエストを送りました。次のポーリングで検収します。")
	return 0
}
