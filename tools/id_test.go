package tools

import (
	"strings"
	"testing"
)

func testSet() Set {
	sha := func(c string) string { return strings.Repeat(c, 64) }
	return Set{
		Tools: []Tool{
			{Name: "ninja", Version: "1", URL: "https://example.invalid/ninja.zip", SHA256: sha("a")},
			{Name: "dfu-util", Version: "0.11", URL: "https://example.invalid/dfu-util-0.11.tar.gz", SHA256: sha("b")},
			{Name: "dtc", Version: "1.8.1", URL: "https://example.invalid/dtc-1.8.1.tar.gz", SHA256: sha("c")},
		},
		Sources: []Tool{
			{Name: "libusb", Version: "1", URL: "https://example.invalid/libusb.tar.bz2", SHA256: sha("d")},
			{Name: "m4", Version: "1", URL: "https://example.invalid/m4.tar.gz", SHA256: sha("e")},
			{Name: "flex", Version: "1", URL: "https://example.invalid/flex.tar.gz", SHA256: sha("f")},
			{Name: "bison", Version: "1", URL: "https://example.invalid/bison.tar.gz", SHA256: sha("1")},
		},
	}
}

func TestIDFollowsEveryInput(t *testing.T) {
	base := ID(testSet())
	reordered := testSet()
	reordered.Tools[0], reordered.Tools[2] = reordered.Tools[2], reordered.Tools[0]
	if ID(reordered) != base {
		t.Fatal("id changed with tool order")
	}
	version := testSet()
	version.Tools[0].Version = "2" // the same bytes: only the sha matters
	if ID(version) != base {
		t.Fatal("id changed with a version label")
	}
	for name, change := range map[string]func(*Set){
		"tool sha":          func(s *Set) { s.Tools[0].SHA256 = strings.Repeat("9", 64) },
		"direct source":     func(s *Set) { s.Sources[0].SHA256 = strings.Repeat("9", 64) },
		"transitive source": func(s *Set) { s.Sources[1].SHA256 = strings.Repeat("9", 64) },
	} {
		s := testSet()
		change(&s)
		if ID(s) == base {
			t.Errorf("%s: id did not change", name)
		}
	}
	old := toolRecipes["ninja"]
	toolRecipes["ninja"] = recipe{kind: old.kind, revision: old.revision + 1}
	defer func() { toolRecipes["ninja"] = old }()
	if ID(testSet()) == base {
		t.Error("recipe revision: id did not change")
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(testSet()); err != nil {
		t.Fatal(err)
	}
	for want, change := range map[string]func(*Set){
		"no install recipe for tool make": func(s *Set) { s.Tools = append(s.Tools, Tool{Name: "make"}) },
		"listed twice":                    func(s *Set) { s.Tools = append(s.Tools, s.Tools[0]) },
		"dtc builds with source":          func(s *Set) { s.Sources = s.Sources[:1] },
		"no build recipe for source zlib": func(s *Set) { s.Sources = append(s.Sources, Tool{Name: "zlib"}) },
		"not 64 hex digits":               func(s *Set) { s.Tools[0].SHA256 = "abc" },
		"https":                           func(s *Set) { s.Tools[0].URL = "http://example.invalid/ninja.zip" },
		"has no version":                  func(s *Set) { s.Tools[0].Version = "" },
	} {
		s := testSet()
		change(&s)
		if err := Validate(s); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}
