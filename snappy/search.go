package snappy

import "strings"

// Search searches all repositories with the given keywords in the args slice
func Search(args []string) (retulst []Part, err error) {
	m := NewMetaRepository()

	return m.Search(strings.Join(args, ","))
}
