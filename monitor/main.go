package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	udev "github.com/pilebones/go-udev"

	"github.com/kr/pretty"
)

func main() {
	conn := new(udev.UEventConn)
	err := conn.Connect()
	if err != nil {
		log.Println("Unable to connect to Netlink Kobject UEvent socket")
		os.Exit(1)
	}
	defer conn.Close()

	queue := make(chan udev.UEvent)
	quit := conn.Monitor(queue, nil)

	// Signal handler to quit properly monitor mode
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-signals
		log.Println("Exiting monitor mode...")
		quit <- true
		os.Exit(0)
	}()

	// Handling message from queue
	for {
		select {
		case uevent := <-queue:
			log.Printf("Handle %s\n", pretty.Sprint(uevent))
		}
	}
}
