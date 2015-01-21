package snappy

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
)

func showInstalledList(installed []Part, showAll bool, o io.Writer) {
	w := tabwriter.NewWriter(o, 5, 3, 1, ' ', 0)
	fmt.Fprintln(w, "Name\tVersion\tSummary\t")
	for _, part := range installed {
		if showAll || part.IsActive() {
			fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t", part.Name(), part.Version(), part.Description()))
		}
	}
	w.Flush()
}

func showUpdatesList(installed []Part, updates []Part, showAll bool, o io.Writer) {
	w := tabwriter.NewWriter(o, 5, 3, 1, ' ', 0)
	fmt.Fprintln(w, "Name\tVersion\tUpdate\t")
	for _, part := range installed {
		if showAll || part.IsActive() {
			update := findPartByName(part.Name(), updates)
			ver := "-"
			if update != nil {
				ver = (*update).Version()
			}
			fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t", part.Name(), part.Version(), ver))
		}
	}
	w.Flush()

}

func CmdList(args []string, showAll bool, showUpdates bool) (err error) {

	m := NewMetaRepository()
	installed, err := m.GetInstalled()
	if err != nil {
		return err
	}
	if showUpdates {
		updates, _ := m.GetUpdates()
		showUpdatesList(installed, updates, showAll, os.Stdout)
	} else {
		showInstalledList(installed, showAll, os.Stdout)
	}

	return err
}
