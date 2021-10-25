package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/tidwall/gjson"
	"github.com/verbit/restvirt-client"
	"github.com/verbit/restvirt-client/pb"
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

var routeTableID uint32

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

	listRoutesResponse, err := client.RouteServiceClient.ListRoutes(context.Background(), &pb.ListRoutesRequest{RouteTableId: routeTableID})
	if err != nil {
		log.Fatalln(err)
	}

	for _, route := range listRoutesResponse.GetRoutes() {
		if _, ok := nexthop[route.Destination]; !ok {
			_, err := client.RouteServiceClient.DeleteRoute(context.Background(), &pb.RouteIdentifier{
				RouteTableId: routeTableID,
				Destination:  route.Destination,
			})
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
		_, err := client.RouteServiceClient.PutRoute(context.Background(), &pb.PutRouteRequest{Route: &pb.Route{
			RouteTableId: routeTableID,
			Destination:  dest,
			Gateways:     gateways,
		}})
		if err != nil {
			log.Fatalln(err)
		}
	}
}

func removeAll(client *restvirt.Client) {
	fmt.Fprintln(os.Stderr, "Removing all routes")
	listRoutesResponse, err := client.RouteServiceClient.ListRoutes(context.Background(), &pb.ListRoutesRequest{RouteTableId: routeTableID})
	if err != nil {
		log.Fatalln(err)
	}

	for _, route := range listRoutesResponse.GetRoutes() {
		_, err := client.RouteServiceClient.DeleteRoute(context.Background(), &pb.RouteIdentifier{
			RouteTableId: routeTableID,
			Destination:  route.Destination,
		})
		if err != nil {
			log.Fatalln(err)
		}
		fmt.Fprintln(os.Stderr, "Removed "+route.Destination)
	}
}

func main() {
	// we exit on shutdown message
	signal.Ignore(syscall.SIGTERM)

	log.SetOutput(os.Stderr)

	if len(os.Args) != 2 {
		log.Fatalln("Usage: restvirt-exabgp <route-table-id>")
	}

	routeTableIDParsed, err := strconv.Atoi(os.Args[1])
	if err != nil {
		log.Fatalln("Couldn't parse route table ID")
	}
	routeTableID = uint32(routeTableIDParsed)

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
