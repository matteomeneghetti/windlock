package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkspacePlan(t *testing.T) {
	root := t.TempDir()
	first := t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "west.yml"), []byte("manifest:\n  self:\n    path: my-firmware\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := planWorkspace(root, first)
	if err != nil {
		t.Fatal(err)
	}
	if plan.selfPath != "my-firmware" {
		t.Fatalf("self.path %s", plan.selfPath)
	}
	wantInit := []string{"init", "-l", "my-firmware"}
	if !reflect.DeepEqual(plan.init, wantInit) {
		t.Fatalf("init %q", plan.init)
	}
	if localWestDir(plan.dir, plan.selfPath) != plan.dir {
		t.Fatalf("west dir %s", localWestDir(plan.dir, plan.selfPath))
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "west.yml"), []byte("manifest:\n  self:\n    path: my-firmware\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := planWorkspace(root, other)
	if err != nil {
		t.Fatal(err)
	}
	if second.dir == plan.dir {
		t.Fatal("second project reused the first project's workspace")
	}
	if _, err := planWorkspace(root, t.TempDir()); err == nil {
		t.Fatal("planned a manifest workspace without west.yml")
	}
}

func TestManifestZephyr(t *testing.T) {
	cases := []struct {
		yml, url, revision string
	}{
		{
			yml: "manifest:\n" +
				"  remotes:\n    - name: zephyrproject-rtos\n      url-base: https://github.com/zephyrproject-rtos\n" +
				"  projects:\n    - name: zephyr\n      remote: zephyrproject-rtos\n      revision: v4.4.1\n      import:\n        name-allowlist: [cmsis_6]\n" +
				"  self:\n    path: my-firmware\n",
			url:      "https://github.com/zephyrproject-rtos/zephyr",
			revision: "v4.4.1",
		},
		{
			yml: "manifest:\n" +
				"  defaults:\n    remote: fork\n    revision: main\n" +
				"  remotes:\n    - name: fork\n      url-base: https://example.com/team/\n" +
				"  projects:\n    - name: hal\n      revision: v1\n    - name: zephyr\n      repo-path: zephyr-fork\n" +
				"  self:\n    path: app\n",
			url:      "https://example.com/team/zephyr-fork",
			revision: "main",
		},
		{
			yml: "manifest:\n" +
				"  projects:\n    - name: zephyr\n      url: https://example.com/z.git\n" +
				"  self:\n    path: app\n",
			url:      "https://example.com/z.git",
			revision: "master",
		},
	}
	for i, tc := range cases {
		path := filepath.Join(t.TempDir(), "west.yml")
		if err := os.WriteFile(path, []byte(tc.yml), 0o644); err != nil {
			t.Fatal(err)
		}
		url, revision, err := manifestZephyr(path)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if url != tc.url || revision != tc.revision {
			t.Fatalf("case %d: url %s revision %s", i, url, revision)
		}
	}
	path := filepath.Join(t.TempDir(), "west.yml")
	if err := os.WriteFile(path, []byte("manifest:\n  projects:\n    - name: hal\n      url: https://example.com/hal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manifestZephyr(path); err == nil {
		t.Fatal("west.yml without zephyr was accepted")
	}
}

func TestManifestLinkStaysOutOfTheProject(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()
	yml := []byte("manifest:\n  self:\n    path: my-firmware\n")
	if err := os.WriteFile(filepath.Join(project, "west.yml"), yml, 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := planWorkspace(root, project)
	if err != nil {
		t.Fatal(err)
	}
	if err := stageLocalManifest(plan.dir, plan.selfPath, project); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(plan.dir, plan.selfPath)
	info, err := os.Lstat(staged)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("staged manifest is a symlink")
	}
	if _, err := os.Stat(filepath.Join(project, ".west")); err == nil {
		t.Fatal("staged init wrote .west into the project")
	}
	if err := replaceWithProjectSymlink(plan.dir, plan.selfPath, project); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(staged)
	if err != nil {
		t.Fatal(err)
	}
	if target != project {
		t.Fatalf("symlink %s", target)
	}
	if _, err := os.Stat(filepath.Join(project, ".west")); err == nil {
		t.Fatal(".west is in the project")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(project), ".west")); err == nil {
		t.Fatal(".west is next to the project")
	}
}
