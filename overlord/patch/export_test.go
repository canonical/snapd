package patch

import (
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// PatchesForTest returns the registered set of patches for testing purposes.
func PatchesForTest() map[int]func(*state.State) error {
	return patches
}

// MockReadInfo replaces patch usage of snap.ReadInfo.
func MockReadInfo(f func(name string, si *snap.SideInfo) (*snap.Info, error)) (restore func()) {
	old := readInfo
	readInfo = f
	return func() { readInfo = old }
}
