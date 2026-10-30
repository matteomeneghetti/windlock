# Host-tool pins

`pins.go` pins one version of each host tool for every platform that uses it, with the URL and sha256 of each archive; OpenOCD, dfu-util and libusb are in `flashtools.go`. Every checksum was checked twice: against the publisher's own checksum (where one exists) and against a fresh download.

## Versions and sources

| Tool | Version | Checksum source |
| --- | --- | --- |
| CMake | 3.31.12 | Kitware `cmake-3.31.12-SHA-256.txt` |
| Ninja | 1.13.2 | Digest GitHub shows for each release asset (the release has no `SHA256SUMS`) |
| CPython | 3.12.15 | astral-sh python-build-standalone release `20261021`, `SHA256SUMS` |
| dtc | 1.8.1 | kernel.org `pub/software/utils/dtc/sha256sums.asc` |
| gperf | 3.3 | GNU gperf 3.3 release announcement |
| OpenOCD | 0.12.0-7 (xPack) | `<asset>.sha` published beside each xPack release asset, and GitHub's asset digest |
| dfu-util | 0.11 | Signed MD5 from dfu-util.sourceforge.net, matched against a fresh download; sha256 matches Homebrew's pin |
| uv | 0.13.6 | `<asset>.sha256` file published beside each release asset, and the digest GitHub shows for it |

dtc's build tools (m4 1.4.21, flex 2.6.4, bison 3.8.2) are pinned too, and so is libusb 1.0.30 (GitHub asset digest, matching Homebrew's pin), which dfu-util links statically. They live under `cache/` and are not part of the host-tools id.

## Why these versions
- **CMake** stays on 3.31, the last 3.x line: CMake 4 rejects projects whose `cmake_minimum_required` is below 3.5. Zephyr v4.4.1's CMake files require 3.20.0.
- **CPython** comes from python-build-standalone (`install_only` builds), because python.org publishes no relocatable Linux archive.
- **uv** resolves and installs each Zephyr entry's Python packages. `uv pip compile --universal` gives one set of pins, valid on every platform, so the lockfile stays the same on every host; pip has no equivalent.

OpenOCD and dfu-util are flash tools: `InstallFlash` installs them on the host, where the probe is on USB, together with Python and uv for the host's west. The container never installs them.

## Per platform
- **linux-x86_64 / linux-aarch64**: what the container installs: CMake, Ninja, Python, uv, and dtc and gperf built from source. OpenOCD and dfu-util are pinned too, for a Linux host's probe side.
- **macos-aarch64 / macos-x86_64**: the probe side only: Python and uv for the host's west, OpenOCD (xPack, one build per CPU) and dfu-util built from source. Nothing that builds firmware runs on the Mac.
- **Windows**: no pins. The launcher compiles for Windows, but the probe side is not ported.

SDK bundles are recorded only for the Linux platforms, the ones the container runs.

## Container image
`ContainerImage` is `ghcr.io/zephyrproject-rtos/ci-base` v0.29.4, pinned by the digest of its OCI image index, which lists linux/amd64 and linux/arm64 (v8). The digest was read from the registry's `Docker-Content-Digest` header and matched against the sha256 of the index bytes. The image supplies only `git`, `make`, a C compiler and the like: windlock installs every pinned tool above inside it, so its own CMake and Python are never used. The arm64 image is about 2.3 GB compressed.

SDK bundles are not pinned here: they come from each SDK release's `sha256.sum` when the lockfile is written.

## Checked
- Linux and macOS archives: 22 Oct 2026.
- uv, every platform, and linux-aarch64 CMake, Ninja and Python: 29 Oct 2026.
- OpenOCD, dfu-util and libusb: 29 Oct 2026.

## Updating a pin
1. Download the new archive and compute its sha256.
2. Compare it with the publisher's checksum file (or GitHub's asset digest).
3. Update `Version`, `URL`, `SHA256` and `File` in `pins.go` or `flashtools.go`, for every platform at once: every platform must pin the same version.
4. Regenerate the golden lockfile: `go test ./setup -run IsIdentical -update`.
5. Run `go test ./...`.
