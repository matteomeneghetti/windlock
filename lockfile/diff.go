package lockfile

import (
	"fmt"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Diff lists every field where got differs from want, one line per field,
// as "path: want X, got Y". Equal lockfiles return nil.
func Diff(want, got Lockfile) ([]string, error) {
	w, err := flatten(want)
	if err != nil {
		return nil, err
	}
	g, err := flatten(got)
	if err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	for p := range w {
		paths[p] = true
	}
	for p := range g {
		paths[p] = true
	}
	var out []string
	for p := range paths {
		wv, wok := w[p]
		gv, gok := g[p]
		switch {
		case wok && gok && wv != gv:
			out = append(out, fmt.Sprintf("%s: want %s, got %s", p, wv, gv))
		case wok && !gok:
			out = append(out, fmt.Sprintf("%s: want %s, got nothing", p, wv))
		case !wok && gok:
			out = append(out, fmt.Sprintf("%s: want nothing, got %s", p, gv))
		}
	}
	sort.Strings(out)
	return out, nil
}

// flatten maps each scalar in lock's YAML to its dotted path, naming list
// items by their name field so an inserted entry doesn't shift later paths.
func flatten(lock Lockfile) (map[string]string, error) {
	var node yaml.Node
	if err := node.Encode(lock); err != nil {
		return nil, err
	}
	out := map[string]string{}
	walk(&node, "", out)
	return out, nil
}

func walk(n *yaml.Node, path string, out map[string]string) {
	join := func(key string) string {
		if path == "" {
			return key
		}
		return path + "." + key
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			walk(c, path, out)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			walk(n.Content[i+1], join(n.Content[i].Value), out)
		}
	case yaml.SequenceNode:
		if len(n.Content) == 0 {
			return
		}
		for i, c := range n.Content {
			key := "[" + strconv.Itoa(i) + "]"
			if name := fieldValue(c, "name"); name != "" {
				key = "[" + name + "]"
			}
			walk(c, path+key, out)
		}
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return
		}
		out[path] = n.Value
	}
}

func fieldValue(n *yaml.Node, key string) string {
	if n.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key && n.Content[i+1].Kind == yaml.ScalarNode {
			return n.Content[i+1].Value
		}
	}
	return ""
}
