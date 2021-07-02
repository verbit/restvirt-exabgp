package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/verbit/restvirt-client"
)

type dummy struct {}
type StringSet map[string]dummy

func NewStringSet() StringSet {
	return make(StringSet)
}

func (s StringSet) append(v string) {
	s[v] = dummy{}
}

func (s StringSet) String() string {
	keys := make([]string, 0, len(s))
	for key, _ := range s {
		keys = append(keys, key)
	}
	return "[" + strings.Join(keys, " ") + "]"
}

var nexthop = make(map[string]StringSet)

func setDefault(m map[string]StringSet, key string, value StringSet) StringSet {
	if v, ok := m[key]; ok {
		return v
	}

	m[key] = value
	return value
}

func main() {
	client, err := restvirt.NewClientFromEnvironment()
	if err != nil {
		log.Fatalf("restvirt client error: %v\n", err)
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(os.Stderr, line)
		j := gjson.Parse(line)
		if j.Get("type").String() != "update" {
			continue
		}
		w := j.Get("neighbor.message.update.withdraw.ipv4 unicast.#.nlri")
		for _, source := range w.Array() {
			fmt.Fprintf(os.Stderr, "- %v\n", source)
			delete(nexthop, source.String())
		}
		r := j.Get("neighbor.message.update.announce.ipv4 unicast")
		for target, result := range r.Map() {
			for _, source := range result.Get("#.nlri").Array() {
				fmt.Fprintf(os.Stderr, "+ %v -> %v\n", source, target)
				targets := setDefault(nexthop, source.String(), NewStringSet())
				targets.append(target)
			}
		}
		fmt.Fprintf(os.Stderr, "= %v\n", nexthop)
	}

	_ = client
}
