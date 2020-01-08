package snapstate

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// Policy encapsulates behaviour that varies with the details of a
// snap installation, like the model assertion or the type of snap
// involved in an operation. Rather than have a forest of `if`s
// looking at type, model, etc, we move it to Policy and look it up.
type Policy interface {
	// CanRemove verifies that a snap can be removed.
	// If rev is not unset, check for removing only that revision.
	CanRemove(st *state.State, snapst *SnapState, rev snap.Revision, dev boot.Device) error
}

var PolicyFor func(snap.Type, *asserts.Model) Policy = policyForUnset

func policyForUnset(snap.Type, *asserts.Model) Policy {
	panic("PolicyFor unset!")
}

func inUseFor(deviceCtx DeviceContext) func(snap.Type) (boot.InUseFunc, error) {
	if deviceCtx == nil {
		return nil
	}
	return func(typ snap.Type) (boot.InUseFunc, error) {
		return boot.InUse(typ, deviceCtx)
	}
}
