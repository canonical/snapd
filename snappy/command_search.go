package snappy

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

func CmdSearch(args []string) (err error) {
	m := NewMetaRepository()
	results, err := m.Search(strings.Join(args, ","))
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 5, 3, 1, ' ', 0)
	fmt.Fprintln(w, "Name\tVersion\tSummary\t")
	for _, part := range results {
		fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t", part.Name(), part.Version(), part.Description()))
	}
	w.Flush()
	return err
}
