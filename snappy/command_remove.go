package snappy

import (
	"fmt"
)

func CmdRemove(args []string) (err error) {

	for _, arg := range args {
		part := GetInstalledSnappByName(arg)
		if part != nil {
			fmt.Printf("Removing %s\n", part.Name())
			err = part.Uninstall()
			if err != nil {
				return err
			}
		}
	}

	return err
}
