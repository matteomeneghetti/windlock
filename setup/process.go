package setup

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"windlock/internal/envvar"
)

func childEnv(hostBin, venv, root, tmp string) []string {
	path := hostBin
	if venv != "" {
		path = filepath.Join(venv, "bin") + string(os.PathListSeparator) + hostBin
	}
	if cur := os.Getenv("PATH"); cur != "" {
		path += string(os.PathListSeparator) + cur
	}
	env := Isolate(os.Environ(), root)
	env = envvar.Set(env, "PATH", path)
	env = envvar.Set(env, "GIT_TERMINAL_PROMPT", "0")
	// west sdk runs CMake, whose FindZephyr-sdk.cmake (v4.4.1) aborts when
	// ZEPHYR_TOOLCHAIN_VARIANT is unset: it tests the variable unquoted.
	env = envvar.Set(env, "ZEPHYR_TOOLCHAIN_VARIANT", "zephyr")
	env = envvar.Unset(env, "ZEPHYR_SDK_INSTALL_DIR")
	if venv != "" {
		env = envvar.Set(env, "VIRTUAL_ENV", venv)
		base := filepath.Join(filepath.Dir(venv), "zephyr")
		if info, err := os.Stat(base); err == nil && info.IsDir() {
			env = envvar.Set(env, "ZEPHYR_BASE", base)
		} else {
			env = envvar.Unset(env, "ZEPHYR_BASE")
		}
	} else {
		env = envvar.Unset(env, "VIRTUAL_ENV")
		env = envvar.Unset(env, "ZEPHYR_BASE")
	}
	if tmp != "" {
		env = envvar.Set(env, "TMPDIR", tmp)
	}
	return env
}

func runStreaming(log io.Writer, dir string, env []string, name string, args ...string) error {
	_, err := runCapture(log, dir, env, name, args...)
	return err
}

func runCapture(log io.Writer, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		cmd.Env = env
	}
	// Through an Output, a child's lines go to the log file, not the terminal.
	out := log
	o, logged := log.(*Output)
	if logged {
		out = o.child()
		if o.file != nil {
			fmt.Fprintf(o.file, "$ %s %s\n", name, strings.Join(args, " "))
		}
	}
	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(out, &buf)
	cmd.Stderr = io.MultiWriter(out, &buf)
	err := cmd.Run()
	if err != nil {
		if logged {
			return buf.String(), o.failure(name, err, buf.String())
		}
		return buf.String(), fmt.Errorf("%s: %w", name, err)
	}
	return buf.String(), nil
}
