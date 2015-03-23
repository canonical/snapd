package snappy

import (
	"io/ioutil"
	"os"
	"strings"
)

var cloudMetaDataFile = "/var/lib/cloud/seed/nocloud-net/meta-data"

// InstallFlags can be used to pass additional flags to the install of a
// snap
type InstallFlags uint

const (
	// AllowUnauthenticated allows to install a snap even if it can not be a uthenticated
	AllowUnauthenticated InstallFlags = 1 << iota
	// InhibitHooks will ensure that the hooks are not run
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
			return part.Install(pbar, flags)
		}
	}

	return ErrPackageNotFound
}
