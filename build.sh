#!/bin/sh
set -eu

cd "$(dirname "$0")"
if [ -d /home/hatch/go-toolchain/go/bin ]; then
    PATH="/home/hatch/go-toolchain/go/bin:$PATH"
fi
export PATH
export GOPROXY=off GOFLAGS=-mod=mod GOTOOLCHAIN=local
export GOMAXPROCS="${GOMAXPROCS:-2}"

(
    cd frontend
    npm ci --no-audit --no-fund
    npm run build
)
go build -o musehood-go .
