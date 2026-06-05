#!/bin/sh
# dad_proxy installer — usage:
#   curl -fsSL https://raw.githubusercontent.com/etspring/dad_proxy/main/install.sh | sh
#   curl -fsSL ... | sh -s -- --version v1.1.2
#   curl -fsSL ... | sh -s -- uninstall

set -eu

REPO="${REPO:-etspring/dad_proxy}"
BIN_NAME="${BIN_NAME:-dad_proxy}"
SERVICE_NAME="${SERVICE_NAME:-dad_proxy}"
WORK_DIR="${WORK_DIR:-/opt/dad_proxy}"
CONFIG_DIR="${CONFIG_DIR:-/etc/dad_proxy}"
ENV_FILE="${ENV_FILE:-${CONFIG_DIR}/dad_proxy.env}"
LOG_DIR="${LOG_DIR:-/var/log/dad_proxy}"
INSTALL_REF="${INSTALL_REF:-main}"

VERSION="${VERSION:-latest}"
USE_ROOT="${USE_ROOT:-0}"
ACTION="install"
TEMP_DIR=""
SUDO=""

PATH="${PATH}:/usr/local/sbin:/usr/local/bin:/usr/sbin:/sbin"

say() { printf '%s\n' "$*"; }
die() { say "error: $*"; exit 1; }

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

init_sudo() {
	if [ "$(id -u)" -eq 0 ]; then
		SUDO=""
		return 0
	fi
	if command -v sudo >/dev/null 2>&1; then
		if sudo -n true 2>/dev/null; then
			SUDO="sudo"
			return 0
		fi
		if [ -t 0 ]; then
			SUDO="sudo"
			return 0
		fi
	fi
	die "run as root or with passwordless sudo"
}

fetch() {
	url="$1"
	out="$2"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url" -o "$out"
	else
		need_cmd wget
		wget -q -O "$out" "$url"
	fi
}

fetch_to_stdout() {
	url="$1"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url"
	else
		need_cmd wget
		wget -q -O - "$url"
	fi
}

detect_os_arch() {
	os="$(uname -s | tr '[:upper:]' '[:lower:]')"
	case "$os" in
	linux) ;;
	*) die "unsupported OS: $os (only linux is supported)" ;;
	esac

	arch="$(uname -m)"
	case "$arch" in
	x86_64|amd64) arch="amd64" ;;
	aarch64|arm64) arch="arm64" ;;
	*) die "unsupported architecture: $arch" ;;
	esac

	printf '%s %s' "$os" "$arch"
}

resolve_version() {
	tag="$VERSION"
	if [ "$tag" = "latest" ]; then
		need_cmd curl
		tag="$(fetch_to_stdout "https://api.github.com/repos/${REPO}/releases/latest" \
			| sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
			| head -n 1)"
		[ -n "$tag" ] || die "could not resolve latest release for ${REPO}"
	fi
	# Allow VERSION=1.1.2 without v prefix
	case "$tag" in
	v*) ;;
	*) tag="v${tag}" ;;
	esac
	VERSION="$tag"
}

download_release_binary() {
	os="$1"
	arch="$2"
	resolve_version

	names="
		${BIN_NAME}-${os}-${arch}.tar.gz
		${BIN_NAME}_${os}_${arch}.tar.gz
		${BIN_NAME}-${os}-${arch}.zip
	"

	for name in $names; do
		url="https://github.com/${REPO}/releases/download/${VERSION}/${name}"
		archive="${TEMP_DIR}/${name}"
		if fetch "$url" "$archive" 2>/dev/null; then
			case "$name" in
			*.tar.gz)
				need_cmd tar
				tar -xzf "$archive" -C "$TEMP_DIR"
				;;
			*.zip)
				need_cmd unzip
				unzip -q -o "$archive" -d "$TEMP_DIR"
				;;
			esac
			bin="$(find "$TEMP_DIR" -type f -name "$BIN_NAME" 2>/dev/null | head -n 1)"
			[ -n "$bin" ] || die "binary ${BIN_NAME} not found in ${name}"
			printf '%s' "$bin"
			return 0
		fi
	done

	die "no release asset found for ${VERSION} (${os}/${arch}); check https://github.com/${REPO}/releases"
}

raw_base() {
	ref="$VERSION"
	if [ "$VERSION" = "latest" ]; then
		ref="$INSTALL_REF"
	fi
	printf 'https://raw.githubusercontent.com/%s/%s' "$REPO" "$ref"
}

ensure_user() {
	if [ "$USE_ROOT" = "1" ]; then
		return 0
	fi
	if id dad_proxy >/dev/null 2>&1; then
		return 0
	fi
	nologin="$(command -v nologin 2>/dev/null || echo /usr/sbin/nologin)"
	if command -v useradd >/dev/null 2>&1; then
		$SUDO useradd --system --no-create-home --shell "$nologin" dad_proxy
	elif command -v adduser >/dev/null 2>&1; then
		$SUDO adduser --system --no-create-home --disabled-password --shell "$nologin" dad_proxy
	else
		die "cannot create user dad_proxy (no useradd/adduser)"
	fi
}

install_env() {
	if [ -f "$ENV_FILE" ]; then
		say "keeping existing config: $ENV_FILE"
		return 0
	fi
	$SUDO mkdir -p "$CONFIG_DIR"
	url="$(raw_base)/deploy/systemd/dad_proxy.env.example"
	tmp="${TEMP_DIR}/dad_proxy.env"
	if fetch "$url" "$tmp" 2>/dev/null; then
		$SUDO install -m 0644 -o root -g root "$tmp" "$ENV_FILE"
	else
		$SUDO tee "$ENV_FILE" >/dev/null <<'EOF'
DAD_PROXY_API_PORT=80
DAD_PROXY_API_HELLO=/dc/helloWorld
DAD_PROXY_PORTS_RANGE=20200,20300
DAD_API_URL=http://live-gateway.lunatichigh.net/dc/helloWorld
DAD_PROXY_IP=127.0.0.1
DAD_PROXY_SHARE=true
DAD_PROXY_ENVIRONMENT=production
DAD_PROXY_UDP_IDLE_TIMEOUT=10m
EOF
		$SUDO chmod 0644 "$ENV_FILE"
	fi
	say "edit $ENV_FILE (set DAD_PROXY_IP to this server's public IP)"
}

install_systemd_unit() {
	unit_path="/etc/systemd/system/${SERVICE_NAME}.service"
	if [ "$USE_ROOT" = "1" ]; then
		unit_src="dad_proxy-root.service"
	else
		unit_src="dad_proxy.service"
	fi
	url="$(raw_base)/deploy/systemd/${unit_src}"
	tmp="${TEMP_DIR}/${unit_src}"
	if ! fetch "$url" "$tmp" 2>/dev/null; then
		die "failed to download systemd unit from ${url}"
	fi
	$SUDO install -m 0644 "$tmp" "$unit_path"
}

setup_dirs() {
	$SUDO mkdir -p "$WORK_DIR" "$CONFIG_DIR" "$LOG_DIR"
	if [ "$USE_ROOT" = "1" ]; then
		$SUDO chown root:root "$WORK_DIR" "$LOG_DIR"
	else
		$SUDO chown dad_proxy:dad_proxy "$WORK_DIR" "$LOG_DIR"
	fi
	$SUDO chmod 755 "$WORK_DIR" "$LOG_DIR"
}

install_binary() {
	src="$1"
	dst="${WORK_DIR}/${BIN_NAME}"
	$SUDO install -m 0755 "$src" "$dst"
	if command -v setcap >/dev/null 2>&1; then
		$SUDO setcap 'cap_net_bind_service=+ep' "$dst" 2>/dev/null || true
	fi
}

start_service() {
	need_cmd systemctl
	$SUDO systemctl daemon-reload
	$SUDO systemctl enable "$SERVICE_NAME"
	if ! $SUDO systemctl restart "$SERVICE_NAME"; then
		say "service failed to start; check: journalctl -u ${SERVICE_NAME} -e"
		return 1
	fi
	return 0
}

do_install() {
	init_sudo
	need_cmd mktemp
	need_cmd find
	command -v curl >/dev/null 2>&1 || need_cmd wget

	TEMP_DIR="$(mktemp -d)"
	trap 'rm -rf "$TEMP_DIR"' EXIT INT HUP TERM

	set -- $(detect_os_arch)
	os="$1"
	arch="$2"

	say "installing ${BIN_NAME} ${VERSION} (${os}/${arch}) from ${REPO}"

	bin="$(download_release_binary "$os" "$arch")"

	ensure_user
	setup_dirs

	if command -v systemctl >/dev/null 2>&1; then
		if $SUDO systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
			$SUDO systemctl stop "$SERVICE_NAME" || true
		fi
	fi

	install_binary "$bin"
	install_env
	install_systemd_unit

	if command -v systemctl >/dev/null 2>&1; then
		start_service
	else
		say "systemd not found; binary installed to ${WORK_DIR}/${BIN_NAME}"
		say "run manually with EnvironmentFile=${ENV_FILE}"
	fi

	say ""
	say "installed: ${WORK_DIR}/${BIN_NAME}"
	say "config:    ${ENV_FILE}"
	say "logs:      ${LOG_DIR}/service.log"
	say "status:    systemctl status ${SERVICE_NAME}"
}

do_uninstall() {
	init_sudo
	if command -v systemctl >/dev/null 2>&1; then
		$SUDO systemctl disable --now "$SERVICE_NAME" 2>/dev/null || true
		$SUDO rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
		$SUDO systemctl daemon-reload 2>/dev/null || true
	fi
	$SUDO rm -f "${WORK_DIR}/${BIN_NAME}"
	say "removed ${SERVICE_NAME} binary and unit (config ${CONFIG_DIR} kept; delete manually if needed)"
}

show_help() {
	cat <<EOF
dad_proxy installer

Usage:
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sh
  sh install.sh [options]

Options:
  install          Install or upgrade (default)
  uninstall        Stop service and remove binary + unit
  -h, --help       Show this help

Environment:
  REPO             GitHub repo (default: ${REPO})
  VERSION          Release tag, e.g. v1.1.2 or latest (default: latest)
  INSTALL_REF      Git ref for systemd/env templates when VERSION=latest (default: main)
  USE_ROOT=1       Run service as root (dad_proxy-root.service)
  DAD_PROXY_*      Not used by installer; set in ${ENV_FILE} after install

Examples:
  VERSION=v1.1.2 sh install.sh
  USE_ROOT=1 curl -fsSL ... | sh
EOF
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
		install) ACTION=install ;;
		uninstall) ACTION=uninstall ;;
		-h|--help|help) ACTION=help ;;
		--version)
			shift
			[ $# -gt 0 ] || die "--version requires a value"
			VERSION="$1"
			;;
		--root) USE_ROOT=1 ;;
		-v|--version-tag)
			shift
			[ $# -gt 0 ] || die "-v requires a value"
			VERSION="$1"
			;;
		*) die "unknown argument: $1 (try --help)" ;;
		esac
		shift
	done
}

parse_args "$@"

case "$ACTION" in
help) show_help ;;
uninstall) do_uninstall ;;
install) do_install ;;
*) die "unknown action: $ACTION" ;;
esac
