# Default tool pins

`pins.go` and `flashtools.go` hold the **defaults** `windlock init` writes into a new lockfile: one version of each tool per platform, with the URL and sha256 of each archive. Every checksum was checked twice: against the publisher's own checksum (where one exists) and against a fresh download.

After `init`, the lockfile is what decides. `sync`, `build` and `flash` install exactly the tools it lists, whatever this windlock's defaults are, and every other command that writes the lockfile keeps its pins. Changing them means rerunning `windlock init`, which puts this windlock's defaults back.

## How tools are installed
Each tool name has a recipe in `recipes.go`, which works for any version the lockfile names:

| Tool | Recipe | Builds with (lockfile `sources`) |
| --- | --- | --- |
| cmake, python, openocd | unpack the archive and merge its prefix | |
| ninja, uv | copy the binary | |
| gperf | configure, make, make install | |
| dtc | make, with flex and bison | flex, bison (and m4, which both need) |
| dfu-util | configure against a static libusb | libusb |

Sources are built into `cache/build-tools/<key>`, and each tool set into `host/<id>`. The key and the id hash every sha256, recipe revision and needed source that goes into them, so a new archive, source or recipe never reuses an old build. Bump a recipe's `revision` when a change to it changes what it installs.

Source builds run in a clean environment (`buildEnv`): a fixed PATH, HOME and TMPDIR under the root, `LANG=C`, `SOURCE_DATE_EPOCH=0`, and none of the user's CC, CFLAGS, LDFLAGS or PKG_CONFIG_PATH. In the container the compiler comes from the pinned image. On a Mac it is the Xcode command line tools' clang, which windlock runs directly (not through the `/usr/bin` shims, whose xcrun writes a cache outside the root) and does not pin. `windlock doctor` shows which clang built the Mac flash tools.

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

dtc's build tools (m4 1.4.21, flex 2.6.4, bison 3.8.2) are pinned too, and so is libusb 1.0.30 (GitHub asset digest, matching Homebrew's pin), which dfu-util links statically. They are the lockfile's `sources`, and the id of every tool built with them includes their sha256.

## Why these versions
- **CMake** stays on 3.31, the last 3.x line: CMake 4 rejects projects whose `cmake_minimum_required` is below 3.5. Zephyr v4.4.1's CMake files require 3.20.0.
- **CPython** comes from python-build-standalone (`install_only` builds), because python.org publishes no relocatable Linux archive.
- **uv** resolves and installs each Zephyr entry's Python packages. `uv pip compile --universal` gives one set of pins, valid on every platform, so the lockfile stays the same on every host; pip has no equivalent.

OpenOCD and dfu-util are flash tools: a host with the probe installs its platform's `flash_tools` (them, plus Python and uv for the host's west). The container never installs them.

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

## Updating a default pin
1. Download the new archive and compute its sha256.
2. Compare it with the publisher's checksum file (or GitHub's asset digest).
3. Update `Version`, `URL` and `SHA256` in `pins.go` or `flashtools.go`. If the new version installs differently, change its recipe in `recipes.go` and bump the recipe's `revision`.
4. Regenerate the golden lockfile: `go test ./setup -run IsIdentical -update`.
5. Run `go test ./...`.

Projects pick the new pins up when they rerun `windlock init`.
