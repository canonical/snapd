package main

import (
	"context"
	"fmt"
	slog "log"
	"os"
	"os/signal"
	"time"

	"github.com/brutella/dnssd"
)

func main() {
	// log.Debug.Enable()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startDNSSDServer(ctx)

	time.Sleep(time.Second)
	go queryDNSSD(ctx)

	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)

		<-stop
		cancel()
	}()

	<-ctx.Done()
}

func queryDNSSD(ctx context.Context) {
	service := "_service_type._tcp.local."

	slog.Printf("Lookup %s\n", service)

	addFn := func(e dnssd.BrowseEntry) {
		slog.Printf(
			"%s can be reached at %s %v\n",
			e.ServiceInstanceName(),
			e.IPs,
			e.Text)
	}

	if err := dnssd.LookupType(ctx, service, addFn, func(dnssd.BrowseEntry) {}); err != nil {
		fmt.Println(err)
		return
	}
}

func startDNSSDServer(ctx context.Context) {
	txtRecord := map[string]string{
		"txtvers": "1",
		"data":    "some-data",
	}
	config := dnssd.Config{
		Name:   "my_service",
		Type:   "_service_type._tcp",
		Domain: "local",
		Port:   1337,
		Text:   txtRecord,
		Ifaces: []string{"en0"},
		// IPs: []net.IP{net.ParseIP("192.168.228.92")},
	}

	service, err := dnssd.NewService(config)
	if err != nil {
		slog.Fatal(err)
	}

	responder, err := dnssd.NewResponder()
	if err != nil {
		slog.Fatal(err)
	}

	_, err = responder.Add(service)
	if err != nil {
		slog.Fatal(err)
	}

	slog.Println("Starting dnssd server")
	err = responder.Respond(ctx)
	if err != nil {
		slog.Fatal(err)
	}
}
