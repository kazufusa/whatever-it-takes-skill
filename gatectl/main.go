// Command gatectl は whatever-it-takes スキルの検収ゲートを制御する。
//
// サブコマンド:
//
//	setup           検収ループをバックグラウンドで起動する
//	run             検収ループ本体 (setupが内部的に起動する。直接使わない)
//	verify          最新の検収結果を確認する
//	stop            検収ループを止める
//	request-check   静穏判定を待たずに次のポーリングで検収させる
//
// 外部パッケージには依存しない。ed25519署名・JSON・プロセス管理はすべて
// 標準ライブラリで完結させている。
package main

import (
	"fmt"
	"os"
)

// version は release ワークフローが `-ldflags "-X main.version=..."` で
// タグ名を埋め込む。ローカルビルド (go build) では "dev" のままになる。
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	var code int
	switch os.Args[1] {
	case "setup":
		code = cmdSetup(os.Args[2:])
	case "run":
		code = cmdRun(os.Args[2:])
	case "verify":
		code = cmdVerify(os.Args[2:])
	case "stop":
		code = cmdStop(os.Args[2:])
	case "request-check":
		code = cmdRequestCheck(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println(version)
		return
	case "-h", "--help", "help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", os.Args[1])
		printUsage()
		code = 2
	}
	os.Exit(code)
}

func printUsage() {
	fmt.Println(`gatectl: whatever-it-takes 検収ゲート制御

使い方:
  gatectl setup --gate-dir DIR --mode mechanical --check-cmd "CMD" [options]
  gatectl setup --gate-dir DIR --mode claude --prompt-file FILE [options]
  gatectl verify --gate-dir DIR
  gatectl request-check --gate-dir DIR
  gatectl stop --gate-dir DIR

各サブコマンドに -h を付けると詳しいオプションを表示する。
詳しい使い方は SKILL.md を参照。`)
}
