package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/tidwall/gjson"
	"github.com/verbit/restvirt-client"
)

type dummy struct{}
type StringSet map[string]dummy

func NewStringSet() StringSet {
	return make(StringSet)
}

func (s StringSet) append(v string) {
	s[v] = dummy{}
}

func (s StringSet) String() string {
	keys := make([]string, 0, len(s))
	for key := range s {
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

var namespace = "bgp"

func update(client *restvirt.Client, j *gjson.Result) {
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

	routes, err := client.GetRoutesInNamespace(namespace)
	if err != nil {
		log.Fatalln(err)
	}

	for _, route := range routes {
		if _, ok := nexthop[route.Destination]; !ok {
			err := client.DeleteRoute(strings.ReplaceAll(route.Destination, "/", "-"))
			if err != nil {
				log.Fatalln(err)
			}
		}
	}

	for dest, hops := range nexthop {
		gateways := make([]string, 0, len(hops))
		for gateway := range hops {
			gateways = append(gateways, gateway)
		}
		err := client.SetRoute(restvirt.Route{
			Destination: dest,
			Namespace:   namespace,
			Gateways:    gateways,
		})
		if err != nil {
			log.Fatalln(err)
		}
	}
}

func removeAll(client *restvirt.Client) {
	fmt.Fprintln(os.Stderr, "Removing all routes")
	routes, err := client.GetRoutesInNamespace(namespace)
	if err != nil {
		log.Fatalln(err)
	}

	for _, route := range routes {
		err := client.DeleteRoute(strings.ReplaceAll(route.Destination, "/", "-"))
		if err != nil {
			log.Fatalln(err)
		}
		fmt.Fprintln(os.Stderr, "Removed "+route.Destination)
	}
}

func main() {
	// we exit on shutdown message
	signal.Ignore(syscall.SIGTERM)

	client, err := restvirt.NewClientFromEnvironment()
	if err != nil {
		log.Fatalf("restvirt client error: %v\n", err)
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(os.Stderr, line)
		j := gjson.Parse(line)
		switch {
		case j.Get("type").String() == "update":
			update(client, &j)
		case j.Get("type").String() == "notification" && j.Get("notification").String() == "shutdown":
			removeAll(client)
			os.Exit(0)
		}
	}
}
