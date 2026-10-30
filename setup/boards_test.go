package setup

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"windlock/lockfile"
)

// testBoardRoots are the fixture project, Zephyr tree, and one module that
// provides a board, laid out like a real workspace.
func testBoardRoots(t *testing.T) []string {
	t.Helper()
	base := filepath.Join("testdata", "boards")
	roots := boardRoots(filepath.Join(base, "app"), filepath.Join(base, "ws"), []lockfile.Project{
		{Name: "vendor", Path: "modules/hal/vendor"},
		{Name: "cmsis_6", Path: "modules/hal/cmsis_6"}, // not cloned: no module.yml
	})
	want := []string{
		filepath.Join(base, "app", "boards"),
		filepath.Join(base, "ws", "zephyr", "boards"),
		filepath.Join(base, "ws", "modules", "hal", "vendor", "boards"),
	}
	if !reflect.DeepEqual(roots, want) {
		t.Fatalf("roots %v, want %v", roots, want)
	}
	return roots
}

func TestResolveBoard(t *testing.T) {
	roots := testBoardRoots(t)
	cases := []struct {
		board, identifier, arch, soc string
	}{
		{"nrf5340dk/nrf5340/cpuapp", "nrf5340dk/nrf5340/cpuapp", "arm", "nrf5340"},
		{"nrf5340dk/nrf5340/cpuapp/ns", "nrf5340dk/nrf5340/cpuapp/ns", "arm", "nrf5340"},
		{"nrf5340dk@1.0.0/nrf5340/cpunet", "nrf5340dk/nrf5340/cpunet", "arm", "nrf5340"},
		{"nrf5340dk//cpuapp", "nrf5340dk/nrf5340/cpuapp", "arm", "nrf5340"},
		{"qemu_cortex_m3", "qemu_cortex_m3", "arm", ""},
		{"twin_b", "twin_b", "x86", ""},
		{"vboard", "vboard", "riscv", ""},
	}
	for _, c := range cases {
		got, err := resolveBoard(roots, c.board)
		if err != nil {
			t.Fatalf("%s: %v", c.board, err)
		}
		if got.Identifier != c.identifier || got.Arch != c.arch || got.SoC != c.soc {
			t.Fatalf("%s: %+v", c.board, got)
		}
	}
}

func TestResolveBoardErrors(t *testing.T) {
	roots := testBoardRoots(t)
	_, err := resolveBoard(roots, "nrf5340dk")
	if err == nil || !strings.Contains(err.Error(),
		"its targets are: nrf5340dk/nrf5340/cpuapp, nrf5340dk/nrf5340/cpuapp/ns, nrf5340dk/nrf5340/cpunet") {
		t.Fatalf("bare multi-target board err %v", err)
	}
	if _, err := resolveBoard(roots, "nrf5340dk/nrf5340/cpuflpr"); err == nil || !strings.Contains(err.Error(), "no target nrf5340dk/nrf5340/cpuflpr") {
		t.Fatalf("unknown qualifier err %v", err)
	}
	if _, err := resolveBoard(roots, "acme_baord"); err == nil || !strings.Contains(err.Error(), "board acme_baord not found (searched: ") {
		t.Fatalf("typo err %v", err)
	}
	// twin_a shares its board.yml with twin_b, but only twin_b has a
	// Twister file, so twin_a is a board with no matching target.
	if _, err := resolveBoard(roots, "twin_a"); err == nil || !strings.Contains(err.Error(), "its targets are: twin_b") {
		t.Fatalf("twin_a err %v", err)
	}
	if _, err := resolveBoard(roots, ""); err == nil {
		t.Fatal("empty board resolved")
	}
}

func TestResolveBoardWithoutTwisterNamesTheFile(t *testing.T) {
	_, err := resolveBoard(testBoardRoots(t), "acme_board")
	if !errors.Is(err, errNoTwister) {
		t.Fatalf("err %v", err)
	}
	want := filepath.Join("testdata", "boards", "app", "boards", "acme", "acme_board", "acme_board.yaml")
	for _, s := range []string{want, "identifier: acme_board\n", "arch: arm", "toolchain:\n  - zephyr"} {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error lacks %q:\n%v", s, err)
		}
	}
	_, err = resolveBoard(testBoardRoots(t), "acme_board/stm32h747xi/m7")
	if !errors.Is(err, errNoTwister) || !strings.Contains(err.Error(), "acme_board_stm32h747xi_m7.yaml") {
		t.Fatalf("qualified missing Twister err %v", err)
	}
}

func TestToolchainsForBoardsUsesTwisterArch(t *testing.T) {
	got, err := toolchainsForBoards(testBoardRoots(t), []string{"vboard", "nrf5340dk/nrf5340/cpunet", "qemu_cortex_m3", "twin_b"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"arm-zephyr-eabi", "x86_64-zephyr-elf", "riscv64-zephyr-elf"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("toolchains %v, want %v", got, want)
	}
	none, err := toolchainsForBoards(testBoardRoots(t), nil)
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("no boards %#v err %v", none, err)
	}
}

// writeBoard adds a board in the real HWMv2 layout under boardsDir: a
// board.yml naming one SoC and a Twister file with the arch.
func writeBoard(t *testing.T, boardsDir, name, arch string) {
	t.Helper()
	writeFile(t, filepath.Join(boardsDir, name, "board.yml"), "board:\n  name: "+name+"\n  socs:\n  - name: s\n")
	writeFile(t, filepath.Join(boardsDir, name, name+".yaml"), "identifier: "+name+"\nname: "+name+"\ntype: mcu\narch: "+arch+"\n")
}

func TestResolveBoardRefusesDuplicates(t *testing.T) {
	app, zephyr := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(app, "boards", "acme", "dup", "board.yml"), "board:\n  name: dup\n")
	writeFile(t, filepath.Join(zephyr, "boards", "other", "dup", "board.yml"), "boards:\n  - name: dup\n  - name: dup_lite\n")
	writeFile(t, filepath.Join(zephyr, "boards", "other", "dup_lite", "board.yml"), "board:\n  name: dup_lite_2\n")
	roots := []string{filepath.Join(app, "boards"), filepath.Join(zephyr, "boards"), filepath.Join(zephyr, "boards")}
	_, err := resolveBoard(roots, "dup")
	if err == nil || !strings.Contains(err.Error(), "defined more than once") ||
		!strings.Contains(err.Error(), filepath.Join(app, "boards", "acme", "dup")) ||
		!strings.Contains(err.Error(), filepath.Join(zephyr, "boards", "other", "dup")) {
		t.Fatalf("err %v", err)
	}
	// A root listed twice is searched once, and dup_lite is defined once.
	if _, _, err := findBoardDir(roots, "dup_lite"); err != nil {
		t.Fatal(err)
	}
}
