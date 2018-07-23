package main

import "fmt"
import "github.com/snapcore/snapd/interfaces/builtin"

func main() {
	for _, iface := range builtin.Interfaces() {
		fmt.Printf("%s\n", iface.Name())
	}
}
