#!/bin/sh
# install.sh — pemasang Musehood Go untuk Linux (opsional service latar).
#
# Pemakaian:
#   ./install.sh                      # pasang binary saja
#   sudo ./install.sh --service       # binary + service (systemd/OpenRC/SysV)
#   sudo ./install.sh --uninstall     # lepas service + binary (data dipertahankan)
#
# Opsi:
#   --version vX.Y.Z    versi rilis GitHub (bawaan: terbaru)
#   --prefix DIR        direktori binary (bawaan: /usr/local/bin sebagai root,
#                       ~/.local/bin bila bukan root)
#   --service           pasang juga service latar (butuh root)
#   --service-name NAMA nama service (bawaan: musehood-go)
#   --user NAMA         user sistem untuk service (bawaan: musehood)
#   --host HOST         host dengar (bawaan: 127.0.0.1)
#   --port PORT         port dengar (bawaan: 3001)
#   --data-dir DIR      direktori data (bawaan: /var/lib/musehood-go)
#   --binary PATH       pakai binary lokal ini, jangan unduh
#   --uninstall         lepas service + binary
#   -h, --help          bantuan ini
#
# Repo GitHub bersifat privat: set GITHUB_TOKEN bila unduhan butuh autentikasi.

set -eu

REPO="afman42/musehood-go"
VERSION=""
PREFIX=""
SERVICE=0
SERVICE_NAME="musehood-go"
SVC_USER="musehood"
HOST="127.0.0.1"
PORT="3001"
DATA_DIR="/var/lib/musehood-go"
LOCAL_BINARY=""
UNINSTALL=0

say() { printf '%s\n' "$*"; }
die() { printf 'Error: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="$2"; shift 2 ;;
        --prefix) PREFIX="$2"; shift 2 ;;
        --service) SERVICE=1; shift ;;
        --service-name) SERVICE_NAME="$2"; shift 2 ;;
        --user) SVC_USER="$2"; shift 2 ;;
        --host) HOST="$2"; shift 2 ;;
        --port) PORT="$2"; shift 2 ;;
        --data-dir) DATA_DIR="$2"; shift 2 ;;
        --binary) LOCAL_BINARY="$2"; shift 2 ;;
        --uninstall) UNINSTALL=1; shift ;;
        -h|--help) sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) die "opsi tidak dikenal: $1 (lihat --help)" ;;
    esac
done

case "$PORT" in
    ''|*[!0-9]*) die "--port harus angka" ;;
esac

IS_ROOT=0
[ "$(id -u)" = "0" ] && IS_ROOT=1

if [ -z "$PREFIX" ]; then
    if [ "$IS_ROOT" = "1" ]; then PREFIX="/usr/local/bin"; else PREFIX="$HOME/.local/bin"; fi
fi
BIN_PATH="$PREFIX/musehood-go"

# ---------- deteksi init system ----------
detect_init() {
    if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
        echo systemd
    elif command -v rc-service >/dev/null 2>&1 || [ -x /sbin/openrc-run ]; then
        echo openrc
    elif [ -d /etc/init.d ]; then
        echo sysv
    else
        echo none
    fi
}

# ---------- uninstall ----------
if [ "$UNINSTALL" = "1" ]; then
    [ "$IS_ROOT" = "1" ] || die "--uninstall butuh root (jalankan dengan sudo)"
    INIT="$(detect_init)"
    case "$INIT" in
        systemd)
            systemctl disable --now "$SERVICE_NAME" 2>/dev/null || true
            rm -f "/etc/systemd/system/$SERVICE_NAME.service"
            systemctl daemon-reload
            ;;
        openrc)
            rc-service "$SERVICE_NAME" stop 2>/dev/null || true
            rc-update del "$SERVICE_NAME" default 2>/dev/null || true
            rm -f "/etc/init.d/$SERVICE_NAME"
            ;;
        sysv)
            "/etc/init.d/$SERVICE_NAME" stop 2>/dev/null || true
            update-rc.d -f "$SERVICE_NAME" remove 2>/dev/null || chkconfig --del "$SERVICE_NAME" 2>/dev/null || true
            rm -f "/etc/init.d/$SERVICE_NAME"
            ;;
    esac
    rm -f "$BIN_PATH"
    say "Service dan binary dilepas. Data di $DATA_DIR dipertahankan."
    exit 0
fi

# ---------- unduh / salin binary ----------
if [ -n "$LOCAL_BINARY" ]; then
    [ -f "$LOCAL_BINARY" ] || die "binary lokal tidak ditemukan: $LOCAL_BINARY"
    mkdir -p "$PREFIX"
    cp "$LOCAL_BINARY" "$BIN_PATH"
else
    ARCH="$(uname -m)"
    case "$ARCH" in
        x86_64|amd64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        i386|i686) ARCH="386" ;;
        armv7l|armv6l|armhf) ARCH="arm" ;;
        riscv64) ARCH="riscv64" ;;
        ppc64le) ARCH="ppc64le" ;;
        ppc64) ARCH="ppc64" ;;
        s390x) ARCH="s390x" ;;
        mips64el) ARCH="mips64le" ;;
        mips64) ARCH="mips64" ;;
        mipsel) ARCH="mipsle" ;;
        mips) ARCH="mips" ;;
        loongarch64) ARCH="loong64" ;;
        *) die "arsitektur tidak dikenal: $ARCH" ;;
    esac
    ASSET="musehood-go-linux-$ARCH"
    if [ -n "$VERSION" ]; then
        URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
    else
        URL="https://github.com/$REPO/releases/latest/download/$ASSET"
    fi
    say "Mengunduh $ASSET ..."
    mkdir -p "$PREFIX"
    TMP="$(mktemp)"
    trap 'rm -f "$TMP"' EXIT
    if command -v curl >/dev/null 2>&1; then
        if [ -n "${GITHUB_TOKEN:-}" ]; then
            curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" -o "$TMP" "$URL" || die "unduhan gagal: $URL"
        else
            curl -fsSL -o "$TMP" "$URL" || die "unduhan gagal: $URL (repo privat? set GITHUB_TOKEN)"
        fi
    elif command -v wget >/dev/null 2>&1; then
        if [ -n "${GITHUB_TOKEN:-}" ]; then
            wget -q --header="Authorization: Bearer $GITHUB_TOKEN" -O "$TMP" "$URL" || die "unduhan gagal: $URL"
        else
            wget -q -O "$TMP" "$URL" || die "unduhan gagal: $URL (repo privat? set GITHUB_TOKEN)"
        fi
    else
        die "butuh curl atau wget untuk mengunduh"
    fi
    mv "$TMP" "$BIN_PATH"
    trap - EXIT
fi
chmod 755 "$BIN_PATH"
say "Binary terpasang: $BIN_PATH ($("$BIN_PATH" -version 2>/dev/null || echo 'versi tidak diketahui'))"

if [ "$SERVICE" = "0" ]; then
    say ""
    say "Jalankan manual: $BIN_PATH -host $HOST -port $PORT -data-dir $DATA_DIR"
    say "Untuk service latar: sudo $0 --service"
    exit 0
fi

# ---------- pasang service ----------
[ "$IS_ROOT" = "1" ] || die "--service butuh root (jalankan dengan sudo)"

if ! id "$SVC_USER" >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --no-create-home --shell /usr/sbin/nologin "$SVC_USER" 2>/dev/null \
            || useradd --system --no-create-home --shell /sbin/nologin "$SVC_USER"
    elif command -v adduser >/dev/null 2>&1; then
        adduser -S -D -H -s /sbin/nologin "$SVC_USER"
    else
        die "tidak bisa membuat user $SVC_USER (useradd/adduser tidak ada)"
    fi
    say "User sistem dibuat: $SVC_USER"
fi
mkdir -p "$DATA_DIR"
chown "$SVC_USER" "$DATA_DIR" 2>/dev/null || chown "$SVC_USER:$SVC_USER" "$DATA_DIR" 2>/dev/null \
    || say "Peringatan: chown $DATA_DIR ke $SVC_USER gagal — pastikan user itu bisa menulis ke direktori tersebut."

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
render() {
    sed -e "s|@BIN@|$BIN_PATH|g" -e "s|@USER@|$SVC_USER|g" \
        -e "s|@DATA_DIR@|$DATA_DIR|g" -e "s|@HOST@|$HOST|g" -e "s|@PORT@|$PORT|g" "$1"
}

INIT="$(detect_init)"
case "$INIT" in
    systemd)
        TEMPLATE="$SCRIPT_DIR/packaging/systemd/musehood-go.service"
        [ -f "$TEMPLATE" ] || die "template tidak ditemukan: $TEMPLATE"
        render "$TEMPLATE" > "/etc/systemd/system/$SERVICE_NAME.service"
        systemctl daemon-reload
        systemctl enable --now "$SERVICE_NAME"
        ;;
    openrc)
        TEMPLATE="$SCRIPT_DIR/packaging/openrc/musehood-go"
        [ -f "$TEMPLATE" ] || die "template tidak ditemukan: $TEMPLATE"
        render "$TEMPLATE" > "/etc/init.d/$SERVICE_NAME"
        chmod +x "/etc/init.d/$SERVICE_NAME"
        rc-update add "$SERVICE_NAME" default
        rc-service "$SERVICE_NAME" start
        ;;
    sysv)
        TEMPLATE="$SCRIPT_DIR/packaging/sysv/musehood-go"
        [ -f "$TEMPLATE" ] || die "template tidak ditemukan: $TEMPLATE"
        render "$TEMPLATE" > "/etc/init.d/$SERVICE_NAME"
        chmod +x "/etc/init.d/$SERVICE_NAME"
        update-rc.d "$SERVICE_NAME" defaults 2>/dev/null || chkconfig --add "$SERVICE_NAME" 2>/dev/null || true
        "/etc/init.d/$SERVICE_NAME" start
        ;;
    *)
        die "init system tidak dikenal — jalankan manual: $BIN_PATH -host $HOST -port $PORT -data-dir $DATA_DIR"
        ;;
esac

# ---------- verifikasi ----------
sleep 2
CHECK_HOST="$HOST"
[ -z "$CHECK_HOST" ] && CHECK_HOST="127.0.0.1"
OK=0
i=0
while [ "$i" -lt 10 ]; do
    if command -v curl >/dev/null 2>&1; then
        curl -fsS "http://$CHECK_HOST:$PORT/api/state" >/dev/null 2>&1 && OK=1 && break
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O /dev/null "http://$CHECK_HOST:$PORT/api/state" && OK=1 && break
    else
        OK=1; break
    fi
    i=$((i + 1)); sleep 1
done
if [ "$OK" = "1" ]; then
    say "Service $SERVICE_NAME berjalan: http://$CHECK_HOST:$PORT"
else
    say "Service terpasang tapi belum menjawab di http://$CHECK_HOST:$PORT — periksa log service."
    exit 1
fi
