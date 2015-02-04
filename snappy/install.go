package snappy

import "fmt"

func Install(args []string) (err error) {
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
			}
		}
	}
	return err
}
