package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// cmdVerify は最新の検収結果を確認する。mode=claude のときは公開鍵で署名を
// 検証したうえで結果を読む。秘密鍵には一切触れない。mode=mechanical のときは
// 署名を使わない。決定的なコマンドの結果は、疑わしければ check-cmd を自分で
// 再実行すればいつでも確かめられるため。
//
// 結果ファイルより後に監視対象 (claudeモードならachievement-dir、mechanical
// モードならproject-dir) が変更されていたら、その結果は古いとみなしPENDING
// 扱いにする。NG→修正→まだ再検収が済んでいない、という状態で前回のOKが
// 残っていると誤って通ってしまうのを防ぐため。
//
// 終了コード:
//
//	0 = 検収OK (現在のコードに対する結果)
//	1 = 検収NG (理由は標準出力の REASON を参照)
//	2 = 署名検証に失敗、または署名ファイルが無い (mode=claudeのみ。結果を信用しない)
//	3 = 検収結果がまだ無い、または結果より後にファイルが変更されていて古い
func cmdVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	gateDir := fs.String("gate-dir", "", "検収ゲートの作業ディレクトリ (必須)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *gateDir == "" {
		fmt.Fprintln(os.Stderr, "error: --gate-dir is required")
		return 2
	}

	cfg, err := loadConfig(*gateDir)
	if err != nil {
		fmt.Printf("PENDING: %s の設定がありません。setupは実行済みですか。\n", *gateDir)
		return 3
	}

	resultsDir := filepath.Join(*gateDir, "results")
	entries, _ := os.ReadDir(resultsDir)
	var jsonFiles []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "result-") && strings.HasSuffix(name, ".json") {
			jsonFiles = append(jsonFiles, name)
		}
	}
	if len(jsonFiles) == 0 {
		fmt.Println("PENDING: 検収結果がまだありません。")
		return 3
	}
	sort.Strings(jsonFiles)
	latest := filepath.Join(resultsDir, jsonFiles[len(jsonFiles)-1])

	data, err := os.ReadFile(latest)
	if err != nil {
		fmt.Printf("PENDING: %s を読めませんでした。\n", latest)
		return 3
	}

	if changed, _ := hasActivitySince(watchTarget(cfg), *gateDir, latest); changed {
		fmt.Printf("PENDING: %s より後にファイルが変更されています。この結果は古く、現在のコードを反映していません。\n", latest)
		return 3
	}

	if cfg.Mode == "claude" {
		pubPath := filepath.Join(*gateDir, "public_key.pem")
		pub, err := publicKeyFromPEMFile(pubPath)
		if err != nil {
			fmt.Printf("UNTRUSTED: 公開鍵を読めません (%s)。この結果は信用できません。\n", pubPath)
			return 2
		}
		sig, err := os.ReadFile(latest + ".sig")
		if err != nil {
			fmt.Printf("UNTRUSTED: %s の署名ファイルがありません。この結果は信用できません。\n", latest)
			return 2
		}
		if !verifyData(pub, data, sig) {
			fmt.Printf("UNTRUSTED: %s の署名検証に失敗しました。この結果は信用できません。\n", latest)
			return 2
		}
	}

	var result struct {
		Timestamp string `json:"timestamp"`
		Verdict   string `json:"verdict"`
		Reason    string `json:"reason"`
		Trigger   string `json:"trigger"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		fmt.Printf("UNTRUSTED: %s の内容を解釈できません。\n", latest)
		return 2
	}

	fmt.Printf("TIMESTAMP: %s\n", result.Timestamp)
	fmt.Printf("VERDICT: %s\n", result.Verdict)
	fmt.Printf("REASON: %s\n", result.Reason)
	fmt.Printf("TRIGGER: %s\n", result.Trigger)
	if cfg.Mode == "claude" {
		fmt.Println("SIGNED: verified")
	} else {
		fmt.Println("SIGNED: n/a (mechanical)")
	}
	fmt.Printf("FILE: %s\n", latest)

	if result.Verdict == "ok" {
		return 0
	}
	return 1
}
