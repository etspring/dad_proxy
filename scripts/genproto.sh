#!/usr/bin/env bash
# Генерирует Go-код из proto/, извлечённых из DungeonCrawler.exe.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROTO_DIR="${ROOT}/proto"
OUT_DIR="${ROOT}/internal/pb"

if ! command -v protoc >/dev/null 2>&1; then
  echo "error: protoc not found (apt install protobuf-compiler)" >&2
  exit 1
fi

export PATH="${PATH}:${HOME}/go/bin}"
if ! command -v protoc-gen-go >/dev/null 2>&1; then
  go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.1
fi

mkdir -p "${OUT_DIR}"

for f in "${PROTO_DIR}"/*.proto; do
  grep -q go_package "$f" || sed -i '/^package /a option go_package = "dad_proxy/internal/pb";' "$f"
done

# Shop.proto отсутствует в exe - Merchant.proto пропускаем
protoc -I "${PROTO_DIR}" \
  --go_out="${OUT_DIR}" --go_opt=paths=source_relative \
  "${PROTO_DIR}/Common.proto" \
  "${PROTO_DIR}/_PacketCommand.proto" \
  "${PROTO_DIR}/_Character.proto" \
  "${PROTO_DIR}/_Chat.proto" \
  "${PROTO_DIR}/_Item.proto" \
  "${PROTO_DIR}/_Defins.proto" \
  "${PROTO_DIR}/Account.proto" \
  "${PROTO_DIR}/Inventory.proto" \
  "${PROTO_DIR}/Lobby.proto" \
  "${PROTO_DIR}/InGame.proto" \
  "${PROTO_DIR}/Party.proto"

for f in "${OUT_DIR}"/_*.go; do
  [ -f "$f" ] || continue
  mv "$f" "${OUT_DIR}/$(basename "${f#_}")"
done

echo "Generated Go protobuf code in ${OUT_DIR}"
