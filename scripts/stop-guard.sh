#!/usr/bin/env bash
# Stopフック用のガード。作業担当が「終わった」と言おうとする直前に、検収ゲート
# の結果を1回だけ確認する。ここでは待たない。判定するだけ。検収そのものに
#時間がかかっても、このスクリプトは即座に返す (待つ設計は、フックがタイムアウト
# したときに出力が捨てられ、ゲートが素通りしてしまう場面で壊れる)。
#
# 終了コード:
#   0 = 停止してよい (検収ゲートを使っていない、検収OK、または安全弁が働いた)
#   2 = 停止できない (理由をstderrに書く。Claude Codeは停止できず会話を続ける)
#
# 安全弁: 連続でブロックした回数が上限を超えたら、無限ループを防ぐために停止を
# 許可する。フックは無限ループを作りうる機構なので、必ず終わり方を用意しておく。
set -uo pipefail

MAX_CONSECUTIVE_BLOCKS=10

PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$(pwd)}"
GATE_DIR="$PROJECT_DIR/.gate"
COUNT_FILE="$GATE_DIR/stop-guard-blocks"

# 検収ゲートを使っていない (whatever-it-takesを使っていない、またはまだ
# setupしていない) セッションでは何もしない。
[ -d "$GATE_DIR" ] || exit 0
[ -f "$GATE_DIR/gatectl-path" ] || exit 0
GATECTL="$(cat "$GATE_DIR/gatectl-path" 2>/dev/null || true)"
[ -n "$GATECTL" ] && [ -x "$GATECTL" ] || exit 0

out="$("$GATECTL" verify --gate-dir "$GATE_DIR" 2>&1)"
code=$?

if [ "$code" -eq 0 ]; then
  rm -f "$COUNT_FILE"
  exit 0
fi

count=0
if [ -f "$COUNT_FILE" ]; then
  count="$(cat "$COUNT_FILE" 2>/dev/null || echo 0)"
fi
case "$count" in ''|*[!0-9]*) count=0 ;; esac
count=$((count + 1))

if [ "$count" -gt "$MAX_CONSECUTIVE_BLOCKS" ]; then
  rm -f "$COUNT_FILE"
  {
    echo "whatever-it-takes: 検収ゲートが${MAX_CONSECUTIVE_BLOCKS}回連続で未完了でした。"
    echo "安全弁として、これ以上はブロックせず停止を許可します。検収は完了していません。"
    echo "ユーザーに、検収が完了していないことを報告してください。"
    echo "$out"
  } >&2
  exit 0
fi
echo "$count" > "$COUNT_FILE"

{
  echo "whatever-it-takes: 検収ゲートがまだOKを出していません。verifyの結果に従って作業を続けてください。"
  echo "$out"
} >&2
exit 2
