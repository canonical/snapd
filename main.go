package main

import (
	"log"
	"os"
)

func main() {
	conn := new(UEventConn)
	err := conn.Connect()
	if err != nil {
		log.Println("Unable to connect to Netlink Kobject UEvent socket")
		os.Exit(1)
	}

	log.Printf("Connection: %#v\n", conn)

	for {
		msg, err := conn.ReadMsg()
		if err != nil {
			log.Println("Unable to read netlink msg, err:", err.Error())
			break
		}

		log.Println("Handle msg:", string(msg))
	}
}
