package patch

import (
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// PatchesForTest returns the registered set of patches for testing purposes.
func PatchesForTest() map[int]func(*state.State) error {
	return patches
}

// MockPatch1ReadType replaces patch1ReadType.
func MockPatch1ReadType(f func(name string, rev snap.Revision) (snap.Type, error)) (restore func()) {
	old := patch1ReadType
	patch1ReadType = f
	return func() { patch1ReadType = old }
}

// MockLevel replaces the current implemented patch level
func MockLevel(lv int) (restorer func()) {
	old := Level
	Level = lv
	return func() { Level = old }
}

func Patch4TaskSnapSetup(task *state.Task) (*patch4SnapSetup, error) {
	return patch4T{}.taskSnapSetup(task)
}

func Patch4StateMap(st *state.State) (map[string]patch4SnapState, error) {
	var stateMap map[string]patch4SnapState
	err := st.Get("snaps", &stateMap)

	return stateMap, err
}

func Patch6StateMap(st *state.State) (map[string]patch6SnapState, error) {
	var stateMap map[string]patch6SnapState
	err := st.Get("snaps", &stateMap)

	return stateMap, err
}

func Patch6SnapSetup(task *state.Task) (patch6SnapSetup, error) {
	var snapsup patch6SnapSetup
	err := task.Get("snap-setup", &snapsup)
	return snapsup, err
}
