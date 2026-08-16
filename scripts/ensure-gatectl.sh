#!/usr/bin/env bash
# gatectl本体を用意する。
#
# 対応プラットフォーム (linux/darwinのamd64/arm64) 向けのプリビルドバイナリを
# 取得する。取得できなければ、Go 1.21以上でその場からビルドする。
# すでに用意済みなら何もしない。
#
# 使い方: ensure-gatectl.sh BASE_DIR
#   BASE_DIR はこのリポジトリ (スキル) のルート。
#   成功すると、gatectl本体の絶対パスを標準出力に1行だけ出す。
# install.shとSKILL.mdのフェーズ1の両方から呼ばれる、共通ロジック。
set -uo pipefail

BASE="${1:?usage: ensure-gatectl.sh BASE_DIR}"
GATECTL="$BASE/bin/gatectl"

if [ -x "$GATECTL" ]; then
  echo "$GATECTL"
  exit 0
fi

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
esac
URL="https://github.com/kazufusa/whatever-it-takes-skill/releases/latest/download/gatectl-${OS}-${ARCH}"

if command -v curl >/dev/null 2>&1 && curl -fsSL -o "$GATECTL" "$URL" 2>/dev/null && [ -s "$GATECTL" ]; then
  chmod +x "$GATECTL"
  echo "$GATECTL"
  exit 0
fi
rm -f "$GATECTL"

if command -v go >/dev/null 2>&1; then
  if ( cd "$BASE" && go build -o bin/gatectl ./cmd/gatectl ) 2>/dev/null; then
    echo "$GATECTL"
    exit 0
  fi
fi

{
  echo "error: gatectl を用意できませんでした。"
  echo "対応プラットフォーム (${OS}/${ARCH}) 向けのプリビルドバイナリを取得できず、"
  echo "Goでのビルドにも失敗しました (${URL})。"
  echo "Go 1.21以上をインストールしてから、もう一度実行してください。"
} >&2
exit 1
