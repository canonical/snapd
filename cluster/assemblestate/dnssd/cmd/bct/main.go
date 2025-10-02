package main

import (
	"bufio"
	"context"
	"fmt"
	"github.com/brutella/dnssd"
	"github.com/brutella/dnssd/log"
	"os"
	"os/signal"
	"strings"
)

func main() {
	if resp, err := dnssd.NewResponder(); err != nil {
		fmt.Println(err)
	} else {

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			for {
				reader := bufio.NewReader(os.Stdin)
				fmt.Print("Enter name \nor\nexit\n>")
				name, _ := reader.ReadString('\n')
				name = strings.Trim(name, "\n")

				if name == "exit" {
					cancel()
					return
				}

				cfg := dnssd.Config{
					Name: name,
					Type: "_asdf._tcp",
					Port: 12345,
				}
				srv, err := dnssd.NewService(cfg)
				if err != nil {
					log.Debug.Fatal(err)
				}
				log.Debug.Printf("%+v\n", srv)
				h, _ := resp.Add(srv)

				<-stop
				resp.Remove(h)
			}
		}()

		if err := resp.Respond(ctx); err != nil {
			fmt.Println(err)
		}
	}
}
