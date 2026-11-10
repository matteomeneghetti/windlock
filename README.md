# windlock

Reproducible Zephyr RTOS builds from one lockfile.

windlock sets up everything a [Zephyr](https://zephyrproject.org) project needs with one command. It builds your firmware in a pinned container and flashes it from your own machine. `windlock.lock.yml` records the exact container image, tools, SDK, Zephyr and module commits, and Python packages, so anyone who clones the project gets the same setup.

## Why

Setting up Zephyr by hand means system packages, a virtual environment, the SDK and environment variables, and every one of those can drift. Two machines with the same source can end up with different toolchains.

West pins Zephyr and its modules, but not the tools around them. windlock pins all of it:

- **One command to get started.** A new teammate or CI runner runs `windlock sync` and has the whole setup.
- **Syncing.** `sync` fails if what it installed doesn't reproduce the lockfile.
- **A clean machine.** Nothing is installed globally. windlock writes only to your project, its own folder (`~/.local/share/windlock`) and the container engine's storage.
- **Flashing from your machine.** Containers can't reach USB probes on macOS, so builds run in the container while flashing and debugging run on your machine, with pinned OpenOCD and dfu-util.

## Install

You'll need:

- Docker, Podman or nerdctl. On a Mac, for example: `brew install colima docker`, then `colima start --cpu 4 --memory 8`
- About 20 GB of free disk space
- A C compiler and `make`, used to build dfu-util the first time you flash
- [Go](https://go.dev), to build windlock

There are no packages yet, so build from source. From a checkout of this repository:

```sh
scripts/build.sh
```

Then put `bin/windlock` on your `PATH`, and run `windlock doctor` to check your setup.

windlock is at an early stage. It has been tested on Apple Silicon Macs, building for and flashing an STM32H7 board with OpenOCD and a J-Link. Debugging and USB DFU haven't been run on a real board yet. It supports Arm, RISC-V, x86 and arm64 boards, but not yet Xtensa (most ESP32 chips), `native_sim`, private repositories, or manifests that import Zephyr through another project, such as the nRF Connect SDK.

## Usage

Create a project and build it:

```sh
windlock new blinky --board nucleo_f401re
cd blinky
windlock build
```

The first run takes several minutes, because it downloads the image and installs the toolchain and Zephyr. After that, builds take seconds. Your project must be in a folder your container engine can see, which by default means under your home folder.

Flash and debug:

```sh
windlock flash -r openocd
windlock debug -r openocd
```

Join a project that already has a lockfile:

```sh
windlock sync
windlock build
```

Add windlock to an existing Zephyr project:

```sh
windlock init
windlock target add app --board <board> --source <dir>
windlock build
```

Commit `windlock.yml` and `windlock.lock.yml`. windlock manages both, so don't edit them by hand. Every tool windlock installs, for building and for flashing, comes from the lockfile, so teammates on different windlock versions still get the same tools. Rerun `windlock init` to move a project to the tool versions your windlock ships with. Run `windlock --help` to see the commands.

## License

[Apache-2.0](LICENSE)
