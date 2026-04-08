#!/usr/bin/env bash
# Build the Firecracker guest agent binary for Linux.
# The agent runs inside microVMs and executes commands via vsock.
#
# Usage: ./scripts/build-guest-agent.sh [output-dir]
#
# The resulting static binary should be placed in the VM rootfs at /usr/local/bin/capyclaw-agent
# and started by the init system (e.g., added to /etc/init.d/ or as PID 1 in a minimal rootfs).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT_DIR="${1:-$PROJECT_ROOT/bin}"

mkdir -p "$OUTPUT_DIR"

echo "Building guest-agent for linux/amd64..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
    -ldflags="-s -w" \
    -o "$OUTPUT_DIR/capyclaw-guest-agent" \
    "$PROJECT_ROOT/internal/burrow/mudbath/guest-agent/"

echo "Built: $OUTPUT_DIR/capyclaw-guest-agent"
echo ""
echo "To use with Firecracker, place this binary in the guest rootfs:"
echo "  cp $OUTPUT_DIR/capyclaw-guest-agent <rootfs>/usr/local/bin/capyclaw-agent"
echo ""
echo "Add to guest init (e.g., /etc/init.d/capyclaw-agent):"
echo "  #!/bin/sh"
echo "  /usr/local/bin/capyclaw-agent &"
