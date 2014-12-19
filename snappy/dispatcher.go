// function dispatcher to handler command-line commands

package snappy

import (
	"fmt"
	"os"
	"sort"
)

type SnappyFunction func(args []string) error

type SnappyCommand struct {
	fp          SnappyFunction
	description string
}

// jump table
var commands map[string]SnappyCommand

func init() {
	// create the jump table
	commands = make(map[string]SnappyCommand)
}

// Add a function to the dispatcher
func registerCommand(cmd string, desc string, f SnappyFunction) {
	commands[cmd] = SnappyCommand{fp: f, description: desc}
}

func showUsage() {
	var keys []string

	// create a slice of command names
	for cmd := range commands {
		keys = append(keys, cmd)
	}

	sort.Strings(keys)

	fmt.Println("Available commands:\n")

	for _, cmd := range keys {
		help := commands[cmd].description
		fmt.Printf("  %s (%s)\n", cmd, help)
	}
	fmt.Println("")
}

func CommandDispatch(cmd string, args []string) (err error) {
	registerCommands()

	return handleCommand(cmd, args)
}

// run the specified command
func handleCommand(cmd string, args []string) (err error) {

	// Special-case
	if cmd == "help" {
		showUsage()
		os.Exit(0)
	}

	command, exists := commands[cmd]

	if !exists {
		fmt.Printf("ERROR: Invalid command: %s\n", cmd)
		showUsage()
		os.Exit(1)
	}

	return command.fp(args)
}
