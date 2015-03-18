package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
)

// InstallFlags can be used to pass additional flags to the install of a
// snap
type InstallFlags uint

const (
	// AllowUnauthenticated allows to install a snap even if it can not be authenticated
	AllowUnauthenticated InstallFlags = 1 << iota
	InhibitHooks
)

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

// Install the givens snap names provided via args. This can be local
// files or snaps that are queried from the store
func Install(name string, flags InstallFlags) (err error) {

	// consume local parts
	if _, err := os.Stat(name); err == nil {
		// we allow unauthenticated package when in developer
		// mode
		var flags InstallFlags
		if inDeveloperMode() {
			flags |= AllowUnauthenticated
		}

		return installClick(name, flags)
	}

	// check repos next
	m := NewMetaRepository()
	found, _ := m.Details(name)
	for _, part := range found {
		// act only on parts that are downloadable
		if !part.IsInstalled() {
			pbar := NewTextProgress(part.Name())
			fmt.Printf("Installing %s\n", part.Name())
			return part.Install(pbar)
		}
	}

	return ErrPackageNotFound
}
