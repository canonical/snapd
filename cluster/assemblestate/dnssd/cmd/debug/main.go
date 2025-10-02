// Command debug logs dns packets to the console.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/brutella/dnssd"
)

var timeFormat = "15:04:05.000"

func main() {
	fmt.Printf("Debugging…\n")
	fmt.Printf("DATE: –––%s–––\n", time.Now().Format("Mon Jan 2 2006"))
	fmt.Printf("%s	...STARTING...\n", time.Now().Format(timeFormat))

	fn := func(req *dnssd.Request) {
		fmt.Println("-------------------------------------------")
		fmt.Printf("%s\n%v\n", time.Now().Format(timeFormat), req)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if rsp, err := dnssd.NewResponder(); err != nil {
		fmt.Println(err)
	} else {
		rsp.Debug(ctx, fn)

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)
		<-stop
		cancel()
	}
}
