package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"launchpad.net/snappy/snappy"
)

type cmdSearch struct {
}

func init() {
	var cmdSearchData cmdSearch
	cmd, _ := parser.AddCommand("search",
		"Search for packages to install",
		"Query the store for available packages",
		&cmdSearchData)

	cmd.Aliases = append(cmd.Aliases, "se")
}

func (x *cmdSearch) Execute(args []string) (err error) {
	return search(args)
}

func search(args []string) error {
	results, err := snappy.Search(args)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 5, 3, 1, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "Name\tVersion\tSummary\t")
	for _, part := range results {
		fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t", part.Name(), part.Version(), part.Description()))
	}

	return nil
}
