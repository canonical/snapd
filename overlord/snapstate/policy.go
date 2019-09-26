package snapstate

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// Policy encapsulates behaviour that varies with the details of a
// snap installation, like the model assertion or the type of snap
// involved in an operation. Rather than have a forest of `if`s
// looking at type, model, etc, we move it to Policy and look it up.
type Policy interface {
	// CanRemove verifies that a snap can be removed.
	//
	// TODO: CanRemove should also return the reason why the snap cannot
	//       be removed to the user
	CanRemove(st *state.State, snapst *SnapState, allRevisions bool) bool
}

var PolicyFor func(snap.Type, *asserts.Model) Policy = policyForUnset

func policyForUnset(snap.Type, *asserts.Model) Policy {
	panic("PolicyFor unset!")
}
