package snapstate

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type Policy interface {
	CanRemove(st *state.State, snapst *SnapState, allRevisions bool) bool
}

var PolicyFor func(snap.Type, *asserts.Model) Policy = policyForUnset

func policyForUnset(snap.Type, *asserts.Model) Policy {
	panic("PolicyFor unset!")
}
