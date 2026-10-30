// Package envvar edits environments in os.Environ's KEY=value form.
package envvar

import "strings"

// Set returns env with key set to val, replacing any earlier value.
func Set(env []string, key, val string) []string {
	return append(Unset(env, key), key+"="+val)
}

// Unset returns env without key.
func Unset(env []string, key string) []string {
	pref := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, pref) {
			out = append(out, e)
		}
	}
	return out
}

// Get returns the value of key in env, or "" when it is unset.
func Get(env []string, key string) string {
	pref := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, pref) {
			return strings.TrimPrefix(e, pref)
		}
	}
	return ""
}
