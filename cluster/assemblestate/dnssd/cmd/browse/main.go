// Command browse browses for specific dns-sd service types.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/brutella/dnssd"
	"github.com/brutella/dnssd/log"
)

var serviceFlag = flag.String("Type", "_asdf._tcp", "Service type")
var domainFlag = flag.String("Domain", "local.", "Browsing domain")
var verboseFlag = flag.Bool("Verbose", false, "Verbose logging")
var timeFormat = "15:04:05.000"

func main() {
	flag.Parse()

	if *verboseFlag {
		log.Debug.Enable()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service := fmt.Sprintf("%s.%s.", strings.Trim(*serviceFlag, "."), strings.Trim(*domainFlag, "."))

	fmt.Printf("Browsing for %s\n", service)
	fmt.Printf("DATE: –––%s–––\n", time.Now().Format("Mon Jan 2 2006"))
	fmt.Printf("%s  ...STARTING...\n", time.Now().Format(timeFormat))
	fmt.Printf("Timestamp	A/R	if Domain	Service Type	Service Name\n")

	addFn := func(e dnssd.BrowseEntry) {
		fmt.Printf("%s	Add	%s	%s	%s	%s (%s)\n", time.Now().Format(timeFormat), e.IfaceName, e.Domain, e.Type, e.Name, e.IPs)
	}

	rmvFn := func(e dnssd.BrowseEntry) {
		fmt.Printf("%s	Rmv	%s	%s	%s	%s\n", time.Now().Format(timeFormat), e.IfaceName, e.Domain, e.Type, e.Name)
	}

	if err := dnssd.LookupType(ctx, service, addFn, rmvFn); err != nil {
		fmt.Println(err)
		return
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	<-stop
	cancel()
}
