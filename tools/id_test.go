package tools

import (
	"reflect"
	"testing"
)

func TestIDStable(t *testing.T) {
	a := []Version{{Name: "ninja", Version: "1"}, {Name: "cmake", Version: "2"}}
	b := []Version{{Name: "cmake", Version: "2"}, {Name: "ninja", Version: "1"}}
	const want = "0bc1d781475855369e2cc4a04ec1fd05327155da0243f4dae35e18f96572eb7b"
	if got := ID(a); got != want {
		t.Fatalf("ID = %s, want %s", got, want)
	}
	if ID(a) != ID(b) {
		t.Fatal("id changed with version order")
	}
	a[0].Version = "9"
	if ID(a) == want {
		t.Fatal("a new ninja version kept the old id")
	}
}

func TestHostToolsID(t *testing.T) {
	const linuxID = "743e2fb6413efb3b7ba1ad235a780c9a258ad838c55e7c2ca8e1cbfff0e54bd2"
	triples := PinnedTriples()
	if !reflect.DeepEqual(triples, []string{"macos-aarch64", "macos-x86_64", "linux-x86_64", "linux-aarch64"}) {
		t.Fatalf("pinned triples %v", triples)
	}
	ids := map[string]string{}
	for _, triple := range triples {
		id, err := IDFor(triple)
		if err != nil {
			t.Fatalf("%s: %v", triple, err)
		}
		ids[triple] = id
	}
	// Both Linux CPUs pin the same versions, so they share an id; the Mac's
	// smaller probe-side set has its own.
	if ids["linux-x86_64"] != linuxID || ids["linux-aarch64"] != linuxID {
		t.Fatalf("linux ids %v", ids)
	}
	if ids["macos-aarch64"] != ids["macos-x86_64"] || ids["macos-aarch64"] == linuxID {
		t.Fatalf("macos ids %v", ids)
	}
}
