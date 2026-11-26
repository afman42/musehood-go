#!/bin/sh
# Build release Musehood Go untuk semua platform (cross-compile).
#
# 1. Build frontend sekali (hasilnya di-embed ke binary Go).
# 2. Cross-compile binary Go untuk semua pasangan GOOS/GOARCH dari
#    `go tool dist list` (CGO_ENABLED=0, tanpa dependensi eksternal).
#
# Output: dist/<os>-<arch>/musehood-go (`.exe` untuk windows).
# Log lengkap: dist/build-release.log
# Platform yang WAJIB berhasil: linux/amd64, linux/arm64, darwin/amd64,
# darwin/arm64, windows/amd64, windows/arm64.
# Pasangan yang bukan executable mandiri (js/wasm, wasip1/wasm) dilewati.
#
# Cara pakai:
#   ./build-release.sh
set -eu

cd "$(dirname "$0")"
if [ -d /home/hatch/go-toolchain/go/bin ]; then
    PATH="/home/hatch/go-toolchain/go/bin:$PATH"
fi
export PATH
export GOPROXY=off GOFLAGS=-mod=mod GOTOOLCHAIN=local CGO_ENABLED=0
export GOMAXPROCS="${GOMAXPROCS:-2}"

# 1. Frontend (sekali).
(
    cd frontend
    npm install --no-audit --no-fund
    npm run build
)

# 2. Cross-compile semua platform.
mkdir -p dist
LOG="dist/build-release.log"
: > "$LOG"
OK=0
FAIL=0
SKIP=0
for pair in $(go tool dist list); do
    os="${pair%/*}"
    arch="${pair#*/}"
    case "$pair" in
        js/wasm|wasip1/wasm)
            echo "SKIP $pair (bukan executable mandiri, butuh runtime JS/WASI)" | tee -a "$LOG"
            SKIP=$((SKIP+1))
            continue
            ;;
    esac
    out="dist/${os}-${arch}/musehood-go"
    case "$os" in
        windows) out="${out}.exe" ;;
    esac
    mkdir -p "dist/${os}-${arch}"
    if GOOS="$os" GOARCH="$arch" go build -ldflags "-X main.version=${VERSION:-dev}" -o "$out" . 2>>"$LOG"; then
        echo "OK   $pair -> $out ($(stat -c%s "$out") byte)" | tee -a "$LOG"
        OK=$((OK+1))
    else
        echo "FAIL $pair (detail di $LOG)" | tee -a "$LOG"
        rm -f "$out"
        FAIL=$((FAIL+1))
    fi
done
echo "---" | tee -a "$LOG"
echo "OK=$OK FAIL=$FAIL SKIP=$SKIP" | tee -a "$LOG"
if [ "$FAIL" -gt 0 ]; then
    echo "PERINGATAN: $FAIL platform gagal (lihat $LOG)" >&2
fi
