package snappy

import (
	"fmt"
	"os"
	"text/tabwriter"
)

func CmdList(args []string, showAll bool) (err error) {

	m := NewMetaRepository()
	installed, err := m.GetInstalled()
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 5, 3, 1, ' ', 0)
	fmt.Fprintln(w, "Name\tVersion\tSummary\t")
	for _, part := range installed {
		if showAll || part.IsActive() {
			fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t", part.Name(), part.Version(), part.Description()))
		}
	}
	w.Flush()

	return err
}
