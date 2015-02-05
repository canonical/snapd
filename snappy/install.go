package snappy

import (
	"fmt"
	"strings"
)

func Install(args []string) (err error) {
	didSomething := false
	m := NewMetaRepository()
	for _, name := range args {
		found, _ := m.Details(name)
		for _, part := range found {
			// act only on parts that are downloadable
			if !part.IsInstalled() {
				pbar := NewTextProgress(part.Name())
				fmt.Printf("Installing %s\n", part.Name())
				err = part.Install(pbar)
				if err != nil {
					return err
				}
				didSomething = true
			}
		}
	}
	if !didSomething {
		return fmt.Errorf("Could not install anything for '%s'", strings.Join(args, ","))
	}

	return err
}
