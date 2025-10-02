// Command register registers a dns-sd service instance.
package main

import (
	"github.com/brutella/dnssd"
	"github.com/brutella/dnssd/log"

	"context"
	"flag"
	"fmt"
	slog "log"
	"os"
	"os/signal"
	"strings"
	"time"
)

var instanceFlag = flag.String("Name", "Service", "Service name")
var serviceFlag = flag.String("Type", "_asdf._tcp", "Service type")
var domainFlag = flag.String("Domain", "local", "domain")
var portFlag = flag.Int("Port", 12345, "Port")
var verboseFlag = flag.Bool("Verbose", false, "Verbose logging")
var interfaceFlag = flag.String("Interface", "", "Network interface name")
var timeFormat = "15:04:05.000"

func main() {
	flag.Parse()
	if len(*instanceFlag) == 0 || len(*serviceFlag) == 0 || len(*domainFlag) == 0 {
		flag.Usage()
		return
	}

	if *verboseFlag {
		log.Debug.Enable()
	}

	instance := fmt.Sprintf("%s.%s.%s.", strings.Trim(*instanceFlag, "."), strings.Trim(*serviceFlag, "."), strings.Trim(*domainFlag, "."))

	fmt.Printf("Registering Service %s port %d\n", instance, *portFlag)
	fmt.Printf("DATE: –––%s–––\n", time.Now().Format("Mon Jan 2 2006"))
	fmt.Printf("%s	...STARTING...\n", time.Now().Format(timeFormat))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if resp, err := dnssd.NewResponder(); err != nil {
		fmt.Println(err)
	} else {
		ifaces := []string{}
		if len(*interfaceFlag) > 0 {
			ifaces = append(ifaces, *interfaceFlag)
		}

		cfg := dnssd.Config{
			Name:   *instanceFlag,
			Type:   *serviceFlag,
			Domain: *domainFlag,
			Port:   *portFlag,
			Ifaces: ifaces,
		}
		srv, err := dnssd.NewService(cfg)
		if err != nil {
			slog.Fatal(err)
		}

		go func() {
			stop := make(chan os.Signal, 1)
			signal.Notify(stop, os.Interrupt)

			<-stop
			cancel()
		}()

		go func() {
			time.Sleep(1 * time.Second)
			handle, err := resp.Add(srv)
			if err != nil {
				fmt.Println(err)
			} else {
				fmt.Printf("%s	Got a reply for service %s: Name now registered and active\n", time.Now().Format(timeFormat), handle.Service().ServiceInstanceName())
			}
		}()
		err = resp.Respond(ctx)

		if err != nil {
			fmt.Println(err)
		}
	}
}
