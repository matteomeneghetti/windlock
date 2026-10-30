#!/bin/sh
# Builds bin/windlock for GOOS/GOARCH (default: this machine). Outside Linux it
# first builds the Linux windlock for the same CPU, which the binary embeds
# and runs inside the container.
set -eu
cd "$(dirname "$0")/.."
goos=${GOOS:-$(go env GOOS)}
goarch=${GOARCH:-$(go env GOARCH)}
rm -f internal/linuxbin/bin/windlock-linux-*
if [ "$goos" != linux ]; then
	CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -trimpath \
		-o "internal/linuxbin/bin/windlock-linux-$goarch" ./cmd/windlock
fi
out=${OUT:-bin/windlock}
if [ "$goos" = windows ]; then
	out=$out.exe
fi
GOOS=$goos GOARCH=$goarch go build -trimpath -o "$out" ./cmd/windlock
rm -f internal/linuxbin/bin/windlock-linux-*
echo "built $out"
