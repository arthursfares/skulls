#!/usr/bin/env bash
# Builds rtc-client and bundles it with the installer into a tarball
# dist/skulls-rtc-client-linux-amd64.tar.gz
set -euo pipefail

CLIENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST_DIR="$CLIENT_DIR/dist"
STAGE="$DIST_DIR/rtc-client"

rm -rf "$STAGE"
mkdir -p "$STAGE"

echo "==> Building skulls-rtc-client (linux/amd64)"
(cd "$CLIENT_DIR" && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o "$STAGE/skulls-rtc-client" .)

cp "$CLIENT_DIR/install.sh" "$STAGE/install.sh"
chmod +x "$STAGE/install.sh" "$STAGE/skulls-rtc-client"

echo "==> Packaging tarball"
(cd "$DIST_DIR" && tar -czf skulls-rtc-client-linux-amd64.tar.gz rtc-client)

echo ""
echo "Done: $DIST_DIR/skulls-rtc-client-linux-amd64.tar.gz"
echo "To install, run:"
echo "  tar -xzf skulls-rtc-client-linux-amd64.tar.gz && cd rtc-client && ./install.sh"
