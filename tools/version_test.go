package tools

import "testing"

func TestAtLeast(t *testing.T) {
	cases := []struct {
		got, floor string
		want       bool
	}{
		{"cmake version 3.31.12", "3.20.5", true},
		{"3.20.5", "3.20.5", true},
		{"3.20.4", "3.20.5", false},
		{"3.20", "3.20.5", false},
		{"Version: DTC 1.8.1", "1.4.6", true},
		{"1.4.6", "1.4.6", true},
		{"1.4.5", "1.4.6", false},
	}
	for _, tc := range cases {
		ok, err := atLeast(tc.got, tc.floor)
		if err != nil {
			t.Errorf("atLeast(%q, %q): %v", tc.got, tc.floor, err)
			continue
		}
		if ok != tc.want {
			t.Errorf("atLeast(%q, %q) = %v, want %v", tc.got, tc.floor, ok, tc.want)
		}
	}
}

func TestPython312(t *testing.T) {
	if err := requirePython312("Python 3.12.15"); err != nil {
		t.Fatal(err)
	}
	if err := requirePython312("Python 3.12"); err != nil {
		t.Fatal(err)
	}
	if err := requirePython312("Python 3.13.0"); err == nil {
		t.Fatal("3.13 was accepted")
	}
	if err := requirePython312("Python 3.11.9"); err == nil {
		t.Fatal("3.11 was accepted")
	}
}
