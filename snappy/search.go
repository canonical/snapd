package snappy

import "strings"

func Search(args []string) (retulst []Part, err error) {
	m := NewMetaRepository()

	return m.Search(strings.Join(args, ","))
}
