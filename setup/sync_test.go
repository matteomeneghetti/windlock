package setup

import (
	"io"
	"strings"
	"testing"

	"github.com/matteomeneghetti/windlock/lockfile"
)

func TestConfigMatches(t *testing.T) {
	lock := mustLock(t, "linux-x86_64")
	cfg := lockfile.Config{Zephyr: []string{"v4.4.2", "v4.3.1"}}
	if err := configMatches(cfg, lock, false); err != nil {
		t.Fatal(err)
	}
	reordered := lockfile.Config{Zephyr: []string{"v4.3.1", "v4.4.2"}}
	if err := configMatches(reordered, lock, false); err == nil || !strings.Contains(err.Error(), "windlock init") {
		t.Fatalf("reordered err %v", err)
	}
	added := lockfile.Config{Zephyr: []string{"v4.4.2", "v4.3.1", "main"}}
	if err := configMatches(added, lock, false); err == nil || !strings.Contains(err.Error(), "main") {
		t.Fatalf("added err %v", err)
	}
	if err := configMatches(lockfile.Config{}, lock, true); err != nil {
		t.Fatalf("manifest err %v", err)
	}
	if err := configMatches(cfg, lock, true); err == nil || !strings.Contains(err.Error(), "west.yml") {
		t.Fatalf("manifest with revisions err %v", err)
	}
}

func TestCheckBundles(t *testing.T) {
	lock := mustLock(t, "linux-x86_64")
	for _, triple := range []string{"linux-x86_64", "linux-aarch64"} {
		if err := checkBundles(lock, testSums, triple, io.Discard); err != nil {
			t.Fatalf("%s: %v", triple, err)
		}
	}
	bad := mustLock(t, "linux-x86_64")
	b := bad.Platforms["linux-aarch64"].SDK["1.0.1"]
	b.SHA256 = strings.Repeat("00", 32)
	bad.Platforms["linux-aarch64"].SDK["1.0.1"] = b
	if err := checkBundles(bad, testSums, "linux-aarch64", io.Discard); err == nil || !strings.Contains(err.Error(), "linux-aarch64") {
		t.Fatalf("mismatch err %v", err)
	}
}

// What sync reports when a host observes something the lockfile did not
// record: the regenerated lockfile's diff names the field.
func TestRegeneratedDiffNamesTheField(t *testing.T) {
	lock := mustLock(t, "linux-x86_64")
	observed := testEntries()
	observed[0].Projects[0].Revision = strings.Repeat("d", 40)
	regenerated, err := assembleLock(observed, lock.West, testSums, "linux-aarch64")
	if err != nil {
		t.Fatal(err)
	}
	diff, err := lockfile.Diff(lock, regenerated)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) != 1 || !strings.HasPrefix(diff[0], "zephyr[4.4.2].projects[cmsis_6].revision: want cccc") {
		t.Fatalf("diff %v", diff)
	}
}
