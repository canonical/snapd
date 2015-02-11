package main

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"launchpad.net/snappy/snappy"
)

type CmdList struct {
	Updates bool `short:"u" long:"updates" description:"Show available updates"`
	ShowAll bool `short:"a" long:"all" description:"Show all parts"`
}

var cmdList CmdList

func init() {
	cmd, _ := Parser.AddCommand("list",
		"List installed parts",
		"Shows all installed parts",
		&cmdList)

	cmd.Aliases = append(cmd.Aliases, "li")
}

func (x *CmdList) Execute(args []string) (err error) {
	return x.list()
}

func (x CmdList) list() error {
	installed, err := snappy.ListInstalled()
	if err != nil {
		return err
	}

	if x.Updates {
		updates, err := snappy.ListUpdates()
		if err != nil {
			return err
		}
		showUpdatesList(installed, updates, x.ShowAll, os.Stdout)
	} else {
		showInstalledList(installed, x.ShowAll, os.Stdout)
	}

	return err
}

func showInstalledList(installed []snappy.Part, showAll bool, o io.Writer) {
	w := tabwriter.NewWriter(o, 5, 3, 1, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "Name\tVersion\tSummary\t")
	for _, part := range installed {
		if showAll || part.IsActive() {
			fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t", part.Name(), part.Version(), part.Description()))
		}
	}
}

func showUpdatesList(installed []snappy.Part, updates []snappy.Part, showAll bool, o io.Writer) {
	w := tabwriter.NewWriter(o, 5, 3, 1, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "Name\tVersion\tUpdate\t")
	for _, part := range installed {
		if showAll || part.IsActive() {
			ver := "-"
			update := snappy.FindSnapsByName(part.Name(), updates)
			if len(update) == 1 {
				ver = update[0].Version()
			}
			fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t", part.Name(), part.Version(), ver))
		}
	}
}
