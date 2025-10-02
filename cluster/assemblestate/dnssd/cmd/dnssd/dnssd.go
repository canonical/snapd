// dnssd is a utilty to register and browser DNS-SD services.
package main

import (
	"github.com/brutella/dnssd"
	"github.com/brutella/dnssd/log"

	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

var nameFlag = flag.String("Name", "", "Service Name")
var typeFlag = flag.String("Type", "", "Service type")
var domainFlag = flag.String("Domain", "local", "Service Domain")
var hostFlag = flag.String("Host", "", "Hostname")
var ipFlag = flag.String("IP", "", "")
var portFlag = flag.Int("Port", 0, "")
var interfaceFlag = flag.String("Interface", "", "")
var timeFormat = "15:04:05.000"
var verboseFlag = flag.Bool("Verbose", false, "Verbose logging")

// Name of the invoked executable.
var name = filepath.Base(os.Args[0])

func printUsage() {
	log.Info.Println("A DNS-SD utilty to register, browse and resolve Bonjour services.\n\n" +
		"Usage:\n" +
		"  " + name + " register -Name <string> -Type <string> -Port <int> [-Domain <string> -Interface <string[,string]> -Host <string> -IP <string>]\n" +
		"  " + name + " browse                  -Type <string>             [-Domain <string> -Interface <string[,string]>]\n" +
		"  " + name + " resolve  -Name <string> -Type <string>             [-Domain <string> -Interface <string[,string]>]\n")
}

func resolve(typee, instance string) {
	ifaces := parseInterfaceFlag()
	ifaceDesc := "all interfaces"
	if len(ifaces) > 0 {
		ifaceDesc = strings.Join(ifaces, ", ")
	}

	fmt.Printf("Lookup %s at %s\n", instance, ifaceDesc)
	fmt.Printf("DATE: –––%s–––\n", time.Now().Format("Mon Jan 2 2006"))
	fmt.Printf("%s	...STARTING...\n", time.Now().Format(timeFormat))

	addFn := func(e dnssd.BrowseEntry) {
		if e.ServiceInstanceName() == instance {
			text := ""
			for key, value := range e.Text {
				text += fmt.Sprintf("%s=%s", key, value)
			}
			fmt.Printf("%s	%s can be reached at %s.%s.:%d %v\n", time.Now().Format(timeFormat), e.ServiceInstanceName(), e.Host, e.Domain, e.Port, text)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := dnssd.LookupTypeAtInterfaces(ctx, typee, addFn, func(dnssd.BrowseEntry) {}, ifaces...); err != nil {
		fmt.Println(err)
		return
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	<-stop
	cancel()
}

func register(instance string) {
	if *portFlag == 0 {
		log.Info.Println("invalid port", *portFlag)
		printUsage()
		return
	}

	var ips []net.IP
	if *ipFlag != "" {
		ip := net.ParseIP(*ipFlag)
		if ip == nil {
			log.Info.Println("invalid ip", *ipFlag)
			printUsage()
			return
		}
		ips = []net.IP{ip}
	}

	fmt.Printf("Registering Service %s port %d\n", instance, *portFlag)
	fmt.Printf("DATE: –––%s–––\n", time.Now().Format("Mon Jan 2 2006"))
	fmt.Printf("%s	...STARTING...\n", time.Now().Format(timeFormat))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if resp, err := dnssd.NewResponder(); err != nil {
		fmt.Println(err)
	} else {
		cfg := dnssd.Config{
			Name:   *nameFlag,
			Type:   *typeFlag,
			Domain: *domainFlag,
			Port:   *portFlag,
			Ifaces: parseInterfaceFlag(),
			IPs:    ips,
			Host:   *hostFlag,
		}
		srv, err := dnssd.NewService(cfg)
		if err != nil {
			log.Info.Fatal(err)
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

func parseInterfaceFlag() []string {
	ifaces := []string{}
	if len(*interfaceFlag) > 0 {
		for _, iface := range strings.Split(*interfaceFlag, ",") {
			trimmed := strings.TrimSpace(iface)
			if len(trimmed) == 0 {
				continue
			}
			ifaces = append(ifaces, trimmed)
		}
	}

	return ifaces
}

func browse(typee string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ifaces := parseInterfaceFlag()
	ifaceDesc := "all interfaces"
	if len(ifaces) > 0 {
		ifaceDesc = strings.Join(ifaces, ", ")
	}

	fmt.Printf("Browsing for %s at %s\n", typee, ifaceDesc)
	fmt.Printf("DATE: –––%s–––\n", time.Now().Format("Mon Jan 2 2006"))
	fmt.Printf("%s  ...STARTING...\n", time.Now().Format(timeFormat))
	fmt.Printf("Timestamp	A/R	if Domain	Service Type	Instance Name\n")

	addFn := func(e dnssd.BrowseEntry) {
		fmt.Printf("%s	Add	%s	%s	%s	%s (%s)\n", time.Now().Format(timeFormat), e.IfaceName, e.Domain, e.Type, e.Name, e.IPs)
	}

	rmvFn := func(e dnssd.BrowseEntry) {
		fmt.Printf("%s	Rmv	%s	%s	%s	%s\n", time.Now().Format(timeFormat), e.IfaceName, e.Domain, e.Type, e.Name)
	}

	if err := dnssd.LookupTypeAtInterfaces(ctx, typee, addFn, rmvFn, ifaces...); err != nil {
		fmt.Println(err)
		return
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	<-stop
	cancel()
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		return
	}

	// The first argument is the command.
	cmd := args[0]

	// Use the remaining arguments as flags.
	flag.CommandLine.Parse(os.Args[2:])

	if *typeFlag == "" {
		printUsage()
		return
	}

	if *verboseFlag {
		log.Debug.Enable()
	}

	typee := fmt.Sprintf("%s.%s.", strings.Trim(*typeFlag, "."), strings.Trim(*domainFlag, "."))
	instance := fmt.Sprintf("%s.%s.%s.", strings.Trim(*nameFlag, "."), strings.Trim(*typeFlag, "."), strings.Trim(*domainFlag, "."))

	switch cmd {
	case "register":
		if *nameFlag == "" {
			printUsage()
			return
		}
		register(instance)
	case "browse":
		browse(typee)
	case "resolve":
		if *nameFlag == "" {
			printUsage()
			return
		}
		resolve(typee, instance)
	default:
		printUsage()
		return
	}
}
