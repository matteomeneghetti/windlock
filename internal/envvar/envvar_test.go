package envvar

import (
	"reflect"
	"testing"
)

func TestSetUnsetGet(t *testing.T) {
	env := []string{"A=1", "PATH=/bin", "AB=2"}
	env = Set(env, "PATH", "/x:/bin")
	if !reflect.DeepEqual(env, []string{"A=1", "AB=2", "PATH=/x:/bin"}) {
		t.Fatalf("set %v", env)
	}
	if Get(env, "A") != "1" || Get(env, "PATH") != "/x:/bin" || Get(env, "B") != "" {
		t.Fatal("get")
	}
	if got := Unset(env, "A"); !reflect.DeepEqual(got, []string{"AB=2", "PATH=/x:/bin"}) {
		t.Fatalf("unset %v", got)
	}
}
