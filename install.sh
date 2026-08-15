#!/usr/bin/env bash
# whatever-it-takes を Claude Code から使えるようにする。
#
# 2通りの実行方法に対応します。
#   - クローンしたリポジトリ内で実行: このリポジトリへのシンボリックリンクを
#     作ります (git pullで更新が反映される、開発向け)。
#   - curlでパイプ実行: 対応プラットフォーム (linux/darwinのamd64/arm64) 向けの
#     リリースtarballをダウンロードして展開します (gitもGoも不要)。
#
#     curl -fsSL https://raw.githubusercontent.com/kazufusa/whatever-it-takes-skill/main/install.sh | bash
#
# sudo権限は使いません。書き込み先はご自身のホームディレクトリか、
# カレントディレクトリの配下だけです。
set -uo pipefail

REPO_SLUG="kazufusa/whatever-it-takes-skill"

# クローン済みリポジトリの中から実行されているかを判定する。
# curlでパイプ実行された場合、BASH_SOURCE は実ファイルを指さない。
SOURCE_DIR=""
if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
  cand="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  if [ -f "$cand/SKILL.md" ] && [ -f "$cand/.claude-plugin/plugin.json" ]; then
    SOURCE_DIR="$cand"
  fi
fi

cat <<'EOF'
whatever-it-takes インストールスクリプト
=========================================

whatever-it-takesは、ユーザー要望に取り組む前に検収ゲートを立て、検収がOKを
出すまで作業を続けるためのClaude Codeスキルです。検収は、終了コードで合否が
決まるmechanicalモードでも、独立したclaude codeセッションに判定させる
claudeモードでも構いません。詳しくはREADME.mdとSKILL.mdを参照してください。

sudo権限は使いません。書き込み先はご自身のホームディレクトリか、カレント
ディレクトリの配下だけです。

EOF

if [ -n "$SOURCE_DIR" ]; then
  echo "クローン済みのリポジトリ ($SOURCE_DIR) の中から実行されています。"
  echo "シンボリックリンクを作ります (git pullで更新が反映されます)。"
else
  echo "リリースからダウンロードして展開します。"
fi
echo

prompt() {
  # $1 をプロンプトとして表示し、1行読んで返す。
  # プロンプト文字列は標準エラーに出す (呼び出し側が標準出力を値として
  # 捕捉できるようにするため)。curlでパイプ実行されると標準入力はスクリプト
  # 本文に使われてしまうので、使える場合は /dev/tty から読む。
  printf '%s' "$1" >&2
  if [ -r /dev/tty ]; then
    read -r REPLY < /dev/tty
  else
    read -r REPLY
  fi
  printf '%s' "$REPLY"
}

choose_target() {
  local personal="$HOME/.claude/skills/whatever-it-takes"
  local project="$(pwd)/.claude/skills/whatever-it-takes"

  {
    echo "インストール先を選んでください。"
    echo "  1) 個人用          $personal"
    echo "     すべてのプロジェクトで使えます。"
    echo "  2) プロジェクト用  $project"
    echo "     このディレクトリ ($(pwd)) のプロジェクトだけで使えます。"
    echo "  3) キャンセル"
    echo
  } >&2

  local choice
  while true; do
    choice="$(prompt "選択 [1-3]: ")"
    case "$choice" in
      1) echo "$personal"; return 0 ;;
      2) echo "$project"; return 0 ;;
      3) return 1 ;;
      *) echo "1、2、3のいずれかを入力してください。" >&2 ;;
    esac
  done
}

# このリポジトリが作ったインストール (シンボリックリンク、または以前の
# リリース展開) かどうかを判定する。
is_our_install() {
  local target="$1"
  [ -f "$target/.claude-plugin/plugin.json" ] || return 1
  grep -q '"name": *"whatever-it-takes"' "$target/.claude-plugin/plugin.json" 2>/dev/null
}

install_from_clone() {
  local target="$1"
  if [ -L "$target" ]; then
    local existing
    existing="$(readlink "$target")"
    if [ "$existing" = "$SOURCE_DIR" ]; then
      echo "すでに $target にインストール済みです。"
      return 0
    fi
    echo "警告: $target はすでに別の場所 ($existing) へのリンクです。" >&2
    local ans
    ans="$(prompt "作り直しますか？ [y/N]: ")"
    case "$ans" in
      y|Y) rm -f "$target" ;;
      *) echo "中止しました。" >&2; return 1 ;;
    esac
  elif [ -e "$target" ]; then
    {
      echo "エラー: $target はシンボリックリンクではない、既存のファイルまたは"
      echo "ディレクトリです。中身を確認してから、もう一度実行してください。"
    } >&2
    return 1
  fi
  mkdir -p "$(dirname "$target")"
  ln -s "$SOURCE_DIR" "$target"
  echo "$target にインストールしました。"
}

install_from_release() {
  local target="$1"
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$arch" in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
  esac
  local asset="whatever-it-takes-${os}-${arch}.tar.gz"
  local url="https://github.com/${REPO_SLUG}/releases/latest/download/${asset}"

  if [ -L "$target" ]; then
    {
      echo "エラー: $target はシンボリックリンクです ($(readlink "$target"))。"
      echo "先に手動で削除してから、もう一度実行してください。"
    } >&2
    return 1
  fi
  if [ -e "$target" ] && ! is_our_install "$target"; then
    {
      echo "エラー: $target はwhatever-it-takes以外の既存のファイルまたは"
      echo "ディレクトリです。中身を確認してから、もう一度実行してください。"
    } >&2
    return 1
  fi

  echo "取得します: $url"
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  if ! curl -fsSL -o "$tmp/bundle.tar.gz" "$url"; then
    {
      echo "エラー: $url を取得できませんでした。"
      echo "対応プラットフォームはlinux/darwinのamd64/arm64だけです。"
      echo "対応外の場合は、リポジトリをクローンして"
      echo "  ./install.sh"
      echo "を実行してください (Goでその場からビルドします)。"
    } >&2
    return 1
  fi
  tar -xzf "$tmp/bundle.tar.gz" -C "$tmp"
  if [ ! -d "$tmp/whatever-it-takes" ]; then
    echo "エラー: ダウンロードした内容が想定と違います。" >&2
    return 1
  fi

  mkdir -p "$(dirname "$target")"
  rm -rf "$target"
  mv "$tmp/whatever-it-takes" "$target"
  chmod +x "$target/bin/gatectl" "$target/install.sh" "$target/scripts/ensure-gatectl.sh"
  echo "$target に展開しました。"
}

TARGET="$(choose_target)" || { echo "キャンセルしました。" >&2; exit 0; }

if [ -n "$SOURCE_DIR" ]; then
  install_from_clone "$TARGET" || exit 1
else
  install_from_release "$TARGET" || exit 1
fi

echo
echo "gatectl本体を用意しています..."
if GATECTL="$("$TARGET/scripts/ensure-gatectl.sh" "$TARGET")"; then
  echo "準備できました: $GATECTL"
  ver="$("$GATECTL" version 2>/dev/null || echo unknown)"
  echo "バージョン: $ver"
else
  echo "gatectlの準備に失敗しました。上のエラーを確認してください。" >&2
  exit 1
fi

echo
echo "インストールが完了しました。"
echo "Claude Codeで /whatever-it-takes と入力するか、「検収ゲートを立てて」の"
echo "ような依頼をすると使えます。"
