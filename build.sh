#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="0.1.0"
ARCH="amd64"
PACKAGE="blockhost-watchdog"
PACKAGE_NAME="${PACKAGE}_${VERSION}_${ARCH}"
BUILD_DIR="${SCRIPT_DIR}/build/${PACKAGE_NAME}"

echo "=== Building ${PACKAGE} ${VERSION} ==="

# Clean previous build
rm -rf "${SCRIPT_DIR}/build"
mkdir -p "$BUILD_DIR"

# --- Compile Go binary ---
echo "Compiling Go binary..."
CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
    go build -ldflags="-s -w" -o "$BUILD_DIR/usr/bin/blockhost-watchdog" "$SCRIPT_DIR"

# --- Create directory structure ---
mkdir -p "$BUILD_DIR"/{DEBIAN,etc/blockhost,usr/bin,usr/lib/systemd/system}

# --- Install files ---
cp "$SCRIPT_DIR/systemd/blockhost-watchdog.service" \
    "$BUILD_DIR/usr/lib/systemd/system/"

cp "$SCRIPT_DIR/config/monitor.yaml" \
    "$BUILD_DIR/etc/blockhost/monitor.yaml"

cp "$SCRIPT_DIR/config/plans.yaml" \
    "$BUILD_DIR/etc/blockhost/plans.yaml"

# --- DEBIAN/control ---
cat > "$BUILD_DIR/DEBIAN/control" << EOF
Package: ${PACKAGE}
Version: ${VERSION}
Section: admin
Priority: optional
Architecture: ${ARCH}
Depends: blockhost-common (>= 0.1.0)
Maintainer: Blockhost Team <blockhost@example.com>
Description: Resource enforcement and health monitoring daemon
 Monitors VM resource usage via provisioner CLI, enforces plan-defined
 resource limits, and detects abuse patterns. Runs continuously on every
 BlockHost host, polling all active VMs within a configurable time budget.
EOF

# --- DEBIAN/conffiles ---
cat > "$BUILD_DIR/DEBIAN/conffiles" << 'EOF'
/etc/blockhost/monitor.yaml
/etc/blockhost/plans.yaml
EOF

# --- DEBIAN/postinst ---
cat > "$BUILD_DIR/DEBIAN/postinst" << 'POSTINST'
#!/bin/bash
set -e

case "$1" in
    configure)
        # Config file permissions (blockhost user/group created by blockhost-common)
        if getent group blockhost > /dev/null 2>&1; then
            for conf in /etc/blockhost/monitor.yaml /etc/blockhost/plans.yaml; do
                if [ -f "$conf" ]; then
                    chown root:blockhost "$conf"
                    chmod 640 "$conf"
                fi
            done
        fi

        systemctl daemon-reload
        systemctl enable blockhost-watchdog

        if systemctl is-system-running --quiet 2>/dev/null; then
            systemctl restart blockhost-watchdog || true
        fi
        ;;
esac

exit 0
POSTINST
chmod 755 "$BUILD_DIR/DEBIAN/postinst"

# --- DEBIAN/prerm ---
cat > "$BUILD_DIR/DEBIAN/prerm" << 'PRERM'
#!/bin/bash
set -e

case "$1" in
    remove|upgrade)
        if systemctl is-active --quiet blockhost-watchdog 2>/dev/null; then
            systemctl stop blockhost-watchdog || true
        fi
        ;;
esac

exit 0
PRERM
chmod 755 "$BUILD_DIR/DEBIAN/prerm"

# --- DEBIAN/postrm ---
cat > "$BUILD_DIR/DEBIAN/postrm" << 'POSTRM'
#!/bin/bash
set -e

case "$1" in
    remove)
        systemctl daemon-reload
        ;;
    purge)
        systemctl daemon-reload
        rm -f /etc/blockhost/monitor.yaml
        rm -f /etc/blockhost/plans.yaml
        ;;
esac

exit 0
POSTRM
chmod 755 "$BUILD_DIR/DEBIAN/postrm"

# --- Build .deb ---
echo "Building .deb package..."
dpkg-deb --build "$BUILD_DIR" "${SCRIPT_DIR}/build/${PACKAGE_NAME}.deb"

echo "=== Built: build/${PACKAGE_NAME}.deb ==="
