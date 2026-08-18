#!/usr/bin/env bash
# Installs rtc-client and its runtime dependencies (libopus, libopusfile, libogg,
# ffmpeg, yt-dlp - the last two are needed for the /play command).
# Run this from inside the extracted rtc-client release directory.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_SRC="$SCRIPT_DIR/skulls-rtc-client"
INSTALL_DIR="${RTC_CLIENT_INSTALL_DIR:-$HOME/.local/bin}"
DEFAULT_SIGNALING_SERVER_URL="https://your-signaling-server.example.com"

if [ ! -f "$BIN_SRC" ]; then
	echo "error: skulls-rtc-client binary not found next to install.sh (expected $BIN_SRC)" >&2
	exit 1
fi

echo "==> Installing runtime dependencies (libopus, libopusfile, libogg, ffmpeg)"

install_with() {
	local mgr="$1"
	case "$mgr" in
	apt-get)
		sudo apt-get update -qq
		sudo apt-get install -y libopus0 libopusfile0 libogg0 pulseaudio-utils ffmpeg
		;;
	dnf)
		sudo dnf install -y opus opusfile libogg ffmpeg
		;;
	pacman)
		sudo pacman -Sy --noconfirm --needed opus opusfile libogg ffmpeg
		;;
	zypper)
		sudo zypper install -y libopus0 libopusfile0 libogg0 ffmpeg
		;;
	esac
}

if command -v apt-get >/dev/null 2>&1; then
	install_with apt-get || echo "warning: dependency install failed, continuing anyway (they may already be installed)" >&2
elif command -v dnf >/dev/null 2>&1; then
	install_with dnf || echo "warning: dependency install failed, continuing anyway (they may already be installed)" >&2
elif command -v pacman >/dev/null 2>&1; then
	install_with pacman || echo "warning: dependency install failed, continuing anyway (they may already be installed)" >&2
elif command -v zypper >/dev/null 2>&1; then
	install_with zypper || echo "warning: dependency install failed, continuing anyway (they may already be installed)" >&2
else
	echo "warning: no supported package manager found (apt-get/dnf/pacman/zypper)." >&2
	echo "         install libopus, libopusfile, libogg and ffmpeg manually, then re-run this script." >&2
fi

echo "==> Installing skulls-rtc-client to $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"
cp "$BIN_SRC" "$INSTALL_DIR/skulls-rtc-client"
chmod +x "$INSTALL_DIR/skulls-rtc-client"

# yt-dlp is the other /play dependency. Distro-packaged builds tend to lag
# behind YouTube's frequent site changes and stop working, so - per yt-dlp's
# own recommendation - install/update the standalone binary release instead,
# right next to rtc-client.
echo "==> Installing yt-dlp to $INSTALL_DIR"
YT_DLP_BIN="$INSTALL_DIR/yt-dlp"
if [ -x "$YT_DLP_BIN" ]; then
	"$YT_DLP_BIN" -U || echo "warning: yt-dlp self-update failed, continuing with existing copy" >&2
elif command -v curl >/dev/null 2>&1; then
	curl -fL "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp" -o "$YT_DLP_BIN" \
		&& chmod +x "$YT_DLP_BIN" \
		|| echo "warning: yt-dlp download failed - install it manually for /play to work" >&2
else
	echo "warning: curl not found - install yt-dlp manually for /play to work" >&2
fi

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	echo ""
	echo "note: $INSTALL_DIR is not on your PATH."
	echo "      add this to your ~/.bashrc or ~/.zshrc:"
	echo "        export PATH=\"\$PATH:$INSTALL_DIR\""
	;;
esac

if [ -z "${SIGNALING_SERVER_URL:-}" ]; then
	SIGNALING_SERVER_URL="$DEFAULT_SIGNALING_SERVER_URL"
	SHELL_PROFILE="$HOME/.bashrc"
	case "${SHELL:-}" in
	*zsh) SHELL_PROFILE="$HOME/.zshrc" ;;
	esac

	if ! grep -q "^export SIGNALING_SERVER_URL=" "$SHELL_PROFILE" 2>/dev/null; then
		echo "" >>"$SHELL_PROFILE"
		echo "export SIGNALING_SERVER_URL=\"$SIGNALING_SERVER_URL\"" >>"$SHELL_PROFILE"
		echo ""
		echo "note: added SIGNALING_SERVER_URL to $SHELL_PROFILE (set to a placeholder)"
		echo "      edit $SHELL_PROFILE and replace it with your actual signaling server URL,"
		echo "      then restart your shell (or run: source $SHELL_PROFILE) before running rtc-client"
	fi
fi

echo ""
echo "Done. Run it with: skulls-rtc-client"
