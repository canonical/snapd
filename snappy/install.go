package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
)

var cloudMetaDataFile = "/var/lib/cloud/seed/nocloud-net/meta-data"

// check if the image is in developer mode
// FIXME: this is a bit crude right now, but it seems like there is not more
//        meta-data to check right now
// TODO: add feature to ubuntu-device-flash to write better info file when
//       the image is in developer mode
func inDeveloperMode() bool {
	f, err := os.Open(cloudMetaDataFile)
	if err != nil {
		return false
	}
	defer f.Close()
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return false
	}
	needle := "public-keys:\n"
	if strings.Contains(string(data), needle) {
		return true
	}
	return false
}

func Install(args []string) (err error) {
	didSomething := false
	m := NewMetaRepository()
	for _, name := range args {

		// consume local parts
		if _, err := os.Stat(name); err == nil {
			// we allow unauthenticated package when in developer
			// mode
			allowUnauthenticated := inDeveloperMode()
			if err := installClick(name, allowUnauthenticated); err != nil {
				return err
			}
			didSomething = true
			continue
		}

		// check repos next
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
