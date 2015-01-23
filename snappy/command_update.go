package snappy

import (
	"fmt"
)

func CmdUpdate(args []string) (err error) {

	r := NewMetaRepository()
	updates, err := r.GetUpdates()
	if err != nil {
		return err
	}
	pbar := NewTextProgress()

	// FIXME: handle args
	for _, part := range updates {
		fmt.Printf("Installing %s (%s)\n", part.Name(), part.Version())
		err := part.Install(pbar)
		if err != nil {
			return err
		}
	}

	return nil
}
