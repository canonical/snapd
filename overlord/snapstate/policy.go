package snapstate

import (
	"errors"

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
	CanRemove(st *state.State, snapst *SnapState, rev snap.Revision, dev snap.Device) error
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

func isCoreBootDevice(st *state.State) (bool, error) {
	// TODO: turn this into a function that takes a DeviceContext
	// that can be nil (TransitionCore case)

	// Use device context just to find out if this is a core boot device.
	// state.ErrNoState is returned if we cannot find the model,
	// which can happen on classic when upgrading from ubuntu-core
	// to core snap (model is set later).
	deviceCtx, err := DeviceCtx(st, nil, nil)
	if err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return false, err
		}
		return false, nil
	}
	return deviceCtx.IsCoreBoot(), nil
}
