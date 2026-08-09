#!/usr/bin/env bash
# ============================================================
#  Build script: compile agentService into agentService
#
#  Source : "$(dirname "$0")/agentService"   (module my.favorite/cclaw)
#  Output : "$(dirname "$0")/bin/agentService"   (no .exe on Linux/macOS)
#  Config : copied to "$(dirname "$0")/bin/config" (binary loads it from there)
#  Usage  : ./build_agentService.sh
#
#  Note   : -trimpath strips local absolute paths from the
#           binary; -ldflags "-s -w" strips symbols to reduce
#           size. Remove them if you need a debuggable binary.
# ============================================================

set -e

# Treat the directory of this script as the SRC root.
SRC_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SRC_ROOT" || {
    echo "[ERROR] Cannot enter directory: $SRC_ROOT" >&2
    exit 1
}

# Check that go is available on PATH.
if ! command -v go >/dev/null 2>&1; then
    echo "[ERROR] \"go\" was not found. Install Go and add it to PATH." >&2
    exit 1
fi

# Output directory.
OUT_DIR="$SRC_ROOT/bin"
mkdir -p "$OUT_DIR"

# Pure-Go build, no C toolchain required.
export CGO_ENABLED=0

echo "[1/3] Building agentService ..."
(
    cd "$SRC_ROOT/agentService"
    go build -trimpath -ldflags "-s -w" -o "$OUT_DIR/agentService" .
)

# Copy config files into bin/config so the binary can load them.
echo "[2/3] Copying config ..."
mkdir -p "$OUT_DIR/config"
cp "$SRC_ROOT/agentService/config/"*.ini "$OUT_DIR/config/"

echo "[3/3] Done: $OUT_DIR/agentService"
exit 0
