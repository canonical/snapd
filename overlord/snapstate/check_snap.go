// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package snapstate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	seccomp_compiler "github.com/snapcore/snapd/sandbox/seccomp"
	"github.com/snapcore/snapd/snap"
)

// featureSet contains the flag values that can be listed in assumes entries
// that this ubuntu-core actually provides.
var featureSet = map[string]bool{
	// Support for common data directory across revisions of a snap.
	"common-data-dir": true,
	// Support for the "Environment:" feature in snap.yaml
	"snap-env": true,
	// Support for the "command-chain" feature for apps and hooks in snap.yaml
	"command-chain": true,
}

// supportedSystemUsernames for now contains the hardcoded list of system
// users (and implied system group of same name) that snaps may specify
var supportedSystemUsernames = map[string]bool{
	"snap_daemon": true,
}

func checkAssumes(si *snap.Info) error {
	missing := ([]string)(nil)
	for _, flag := range si.Assumes {
		if strings.HasPrefix(flag, "snapd") && checkVersion(flag[5:]) {
			continue
		}
		if !featureSet[flag] {
			missing = append(missing, flag)
		}
	}
	if len(missing) > 0 {
		hint := "try to refresh the core snap"
		if release.OnClassic {
			hint = "try to update snapd and refresh the core snap"
		}
		return fmt.Errorf("snap %q assumes unsupported features: %s (%s)", si.InstanceName(), strings.Join(missing, ", "), hint)
	}
	return nil
}

var versionExp = regexp.MustCompile(`^([1-9][0-9]*)(?:\.([0-9]+)(?:\.([0-9]+))?)?`)

func checkVersion(version string) bool {
	req := versionExp.FindStringSubmatch(version)
	if req == nil || req[0] != version {
		return false
	}

	if cmd.Version == "unknown" {
		return true // Development tree.
	}

	cur := versionExp.FindStringSubmatch(cmd.Version)
	if cur == nil {
		return false
	}

	for i := 1; i < len(req); i++ {
		if req[i] == "" {
			return true
		}
		if cur[i] == "" {
			return false
		}
		reqN, err1 := strconv.Atoi(req[i])
		curN, err2 := strconv.Atoi(cur[i])
		if err1 != nil || err2 != nil {
			panic("internal error: version regexp is broken")
		}
		if curN != reqN {
			return curN > reqN
		}
	}

	return true
}

type SnapNeedsDevModeError struct {
	Snap string
}

func (e *SnapNeedsDevModeError) Error() string {
	return fmt.Sprintf("snap %q requires devmode or confinement override", e.Snap)
}

type SnapNeedsClassicError struct {
	Snap string
}

func (e *SnapNeedsClassicError) Error() string {
	return fmt.Sprintf("snap %q requires classic confinement", e.Snap)
}

type SnapNeedsClassicSystemError struct {
	Snap string
}

func (e *SnapNeedsClassicSystemError) Error() string {
	return fmt.Sprintf("snap %q requires classic confinement which is only available on classic systems", e.Snap)
}

type SnapNotClassicError struct {
	Snap string
}

func (e *SnapNotClassicError) Error() string {
	return fmt.Sprintf("snap %q is not a classic confined snap", e.Snap)
}

// determine whether the flags (and system overrides thereof) are
// compatible with the given *snap.Info
func validateFlagsForInfo(info *snap.Info, snapst *SnapState, flags Flags) error {
	if flags.Classic && !info.NeedsClassic() {
		return &SnapNotClassicError{Snap: info.InstanceName()}
	}

	switch c := info.Confinement; c {
	case snap.StrictConfinement, "":
		// strict is always fine
		return nil
	case snap.DevModeConfinement:
		// --devmode needs to be specified every time (==> ignore snapst)
		if flags.DevModeAllowed() {
			return nil
		}
		return &SnapNeedsDevModeError{
			Snap: info.InstanceName(),
		}
	case snap.ClassicConfinement:
		if !release.OnClassic {
			return &SnapNeedsClassicSystemError{Snap: info.InstanceName()}
		}

		if flags.Classic {
			return nil
		}

		if snapst != nil && snapst.Flags.Classic {
			return nil
		}

		return &SnapNeedsClassicError{
			Snap: info.InstanceName(),
		}
	default:
		return fmt.Errorf("unknown confinement %q", c)
	}
}

// do a reasonably lightweight check that a snap described by Info,
// with the given SnapState and the user-specified Flags should be
// installable on the current system.
func validateInfoAndFlags(info *snap.Info, snapst *SnapState, flags Flags) error {
	if err := validateFlagsForInfo(info, snapst, flags); err != nil {
		return err
	}

	// verify we have a valid architecture
	if !arch.IsSupportedArchitecture(info.Architectures) {
		return fmt.Errorf("snap %q supported architectures (%s) are incompatible with this system (%s)", info.InstanceName(), strings.Join(info.Architectures, ", "), arch.UbuntuArchitecture())
	}

	// check assumes
	if err := checkAssumes(info); err != nil {
		return err
	}

	// check system-usernames
	if err := checkSystemUsernames(info); err != nil {
		return err
	}

	return nil
}

var openSnapFile = backend.OpenSnapFile

func validateContainer(c snap.Container, s *snap.Info, logf func(format string, v ...interface{})) error {
	err := snap.ValidateContainer(c, s, logf)
	if err == nil {
		return nil
	}
	return fmt.Errorf("%v; contact developer", err)
}

// checkSnap ensures that the snap can be installed.
func checkSnap(st *state.State, snapFilePath, instanceName string, si *snap.SideInfo, curInfo *snap.Info, flags Flags, deviceCtx DeviceContext) error {
	// This assumes that the snap was already verified or --dangerous was used.

	s, c, err := openSnapFile(snapFilePath, si)
	if err != nil {
		return err
	}

	if err := validateInfoAndFlags(s, nil, flags); err != nil {
		return err
	}

	if err := validateContainer(c, s, logger.Noticef); err != nil {
		return err
	}

	snapName, instanceKey := snap.SplitInstanceName(instanceName)
	// update instance key to what was requested
	s.InstanceKey = instanceKey

	st.Lock()
	defer st.Unlock()

	// allow registered checks to run first as they may produce more
	// precise errors
	for _, check := range checkSnapCallbacks {
		err := check(st, s, curInfo, flags, deviceCtx)
		if err != nil {
			return err
		}
	}

	if snapName != s.SnapName() {
		return fmt.Errorf("cannot install snap %q using instance name %q", s.SnapName(), instanceName)
	}

	return nil
}

// CheckSnapCallback defines callbacks for checking a snap for installation or refresh.
type CheckSnapCallback func(st *state.State, snap, curSnap *snap.Info, flags Flags, deviceCtx DeviceContext) error

var checkSnapCallbacks []CheckSnapCallback

// AddCheckSnapCallback installs a callback to check a snap for installation or refresh.
func AddCheckSnapCallback(check CheckSnapCallback) {
	checkSnapCallbacks = append(checkSnapCallbacks, check)
}

func MockCheckSnapCallbacks(checks []CheckSnapCallback) (restore func()) {
	prev := checkSnapCallbacks
	checkSnapCallbacks = checks
	return func() {
		checkSnapCallbacks = prev
	}
}

func checkSnapdName(st *state.State, snapInfo, curInfo *snap.Info, flags Flags, deviceCtx DeviceContext) error {
	if snapInfo.GetType() != snap.TypeSnapd {
		// not a relevant check
		return nil
	}
	if snapInfo.InstanceName() != "snapd" {
		return fmt.Errorf(`cannot install snap %q of type "snapd" with a name other than "snapd"`, snapInfo.InstanceName())
	}

	return nil
}

func checkCoreName(st *state.State, snapInfo, curInfo *snap.Info, flags Flags, deviceCtx DeviceContext) error {
	if snapInfo.GetType() != snap.TypeOS {
		// not a relevant check
		return nil
	}
	if curInfo != nil {
		// already one of these installed
		return nil
	}
	core, err := coreInfo(st)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}

	// Allow installing "core" even if "ubuntu-core" is already
	// installed. Ideally we should only allow this if we know
	// this install is part of the ubuntu-core->core transition
	// (e.g. via a flag) because if this happens outside of this
	// transition we will end up with not connected interface
	// connections in the "core" snap. But the transition will
	// kick in automatically quickly so an extra flag is overkill.
	if snapInfo.InstanceName() == "core" && core.InstanceName() == "ubuntu-core" {
		return nil
	}

	// but generally do not allow to have two cores installed
	if core.InstanceName() != snapInfo.InstanceName() {
		return fmt.Errorf("cannot install core snap %q when core snap %q is already present", snapInfo.InstanceName(), core.InstanceName())
	}

	return nil
}

func checkGadgetOrKernel(st *state.State, snapInfo, curInfo *snap.Info, flags Flags, deviceCtx DeviceContext) error {
	typ := snapInfo.GetType()
	kind := ""
	var whichName func(*asserts.Model) string
	switch typ {
	case snap.TypeGadget:
		kind = "gadget"
		whichName = (*asserts.Model).Gadget
	case snap.TypeKernel:
		kind = "kernel"
		whichName = (*asserts.Model).Kernel
	default:
		// not a relevant check
		return nil
	}

	ok, err := HasSnapOfType(st, typ)
	if err != nil {
		return fmt.Errorf("cannot detect original %s snap: %v", kind, err)
	}
	// in firstboot we have no gadget/kernel yet - that is ok
	// first install rules are in devicestate!
	if !ok {
		return nil
	}

	currentSnap, err := infoForDeviceSnap(st, deviceCtx, kind, whichName)
	if err == state.ErrNoState {
		// TODO: remodeling logic
		return fmt.Errorf("internal error: cannot remodel kernel/gadget yet")
	}
	if err != nil {
		return fmt.Errorf("cannot find original %s snap: %v", kind, err)
	}

	if currentSnap.SnapID != "" && snapInfo.SnapID != "" {
		if currentSnap.SnapID == snapInfo.SnapID {
			// same snap
			return nil
		}
		return fmt.Errorf("cannot replace %s snap with a different one", kind)
	}

	if currentSnap.SnapID != "" && snapInfo.SnapID == "" {
		return fmt.Errorf("cannot replace signed %s snap with an unasserted one", kind)
	}

	if currentSnap.InstanceName() != snapInfo.InstanceName() {
		return fmt.Errorf("cannot replace %s snap with a different one", kind)
	}

	return nil
}

func checkBases(st *state.State, snapInfo, curInfo *snap.Info, flags Flags, deviceCtx DeviceContext) error {
	// check if this is relevant
	if snapInfo.GetType() != snap.TypeApp && snapInfo.GetType() != snap.TypeGadget {
		return nil
	}
	if snapInfo.Base == "" {
		return nil
	}
	if snapInfo.Base == "none" {
		return nil
	}

	snapStates, err := All(st)
	if err != nil {
		return err
	}
	for otherSnap, snapst := range snapStates {
		typ, err := snapst.Type()
		if err != nil {
			return err
		}
		if typ == snap.TypeBase && otherSnap == snapInfo.Base {
			return nil
		}
		// core can be used instead for core16
		if snapInfo.Base == "core16" && otherSnap == "core" {
			return nil
		}
	}

	return fmt.Errorf("cannot find required base %q", snapInfo.Base)
}

func checkEpochs(_ *state.State, snapInfo, curInfo *snap.Info, _ Flags, deviceCtx DeviceContext) error {
	if curInfo == nil {
		return nil
	}
	if snapInfo.Epoch.CanRead(curInfo.Epoch) {
		return nil
	}
	desc := "local snap"
	if snapInfo.SideInfo.Revision.Store() {
		desc = fmt.Sprintf("new revision %s", snapInfo.SideInfo.Revision)
	}

	return fmt.Errorf("cannot refresh %q to %s with epoch %s, because it can't read the current epoch of %s", snapInfo.InstanceName(), desc, snapInfo.Epoch, curInfo.Epoch)
}

// check that the snap installed in the system (via snapst) can be
// upgraded to info (i.e. that info's epoch can read sanpst's epoch)
func earlyEpochCheck(info *snap.Info, snapst *SnapState) error {
	if snapst == nil {
		// no snapst, no problem
		return nil
	}
	cur, err := snapst.CurrentInfo()
	if err != nil {
		if err == ErrNoCurrent {
			// refreshing a disabled snap (maybe via InstallPath)
			return nil
		}
		return err
	}

	return checkEpochs(nil, info, cur, Flags{}, nil)
}

// check that the listed system users are valid
var (
	findUid = osutil.FindUid
	findGid = osutil.FindGid
)

// TODO: keep this unsupported until it is complete!
var systemUsernamesSupported = false

func checkSystemUsernames(si *snap.Info) error {
	// TODO: keep this unsupported until it is complete!
	if !systemUsernamesSupported && len(si.SystemUsernames) > 0 {
		return fmt.Errorf("system usernames are not yet supported")
	}

	// No need to check support if no system-usernames
	if len(si.SystemUsernames) == 0 {
		return nil
	}

	// Run /.../snap-seccomp version-info
	vi, err := seccomp_compiler.CompilerVersionInfo(cmd.InternalToolPath)
	if err != nil {
		return fmt.Errorf("cannot obtain seccomp compiler information: %v", err)
	}

	// If the system doesn't support robust argument filtering then we
	// can't support system-usernames
	if err := vi.SupportsRobustArgumentFiltering(); err != nil {
		if re, ok := err.(*seccomp_compiler.BuildTimeRequirementError); ok {
			return fmt.Errorf("snap %q system usernames require a snapd built against %s", si.InstanceName(), re.RequirementsString())
		}
		return err
	}

	for _, user := range si.SystemUsernames {
		if !supportedSystemUsernames[user.Name] {
			return fmt.Errorf(`snap %q requires unsupported system username "%s"`, si.InstanceName(), user.Name)
		}

		switch user.Scope {
		case "shared":
			_, uidErr := findUid(user.Name)
			_, gidErr := findGid(user.Name)
			if uidErr != nil || gidErr != nil {
				return fmt.Errorf(`snap %q requires that both the "%s" system user and group are present on the system.`, si.InstanceName(), user.Name)
			}
		case "private", "external":
			return fmt.Errorf(`snap %q requires unsupported user scope "%s" for this version of snapd`, si.InstanceName(), user.Scope)
		default:
			return fmt.Errorf(`snap %q requires unsupported user scope "%s"`, si.InstanceName(), user.Scope)
		}
	}
	return nil
}

func init() {
	AddCheckSnapCallback(checkCoreName)
	AddCheckSnapCallback(checkSnapdName)
	AddCheckSnapCallback(checkGadgetOrKernel)
	AddCheckSnapCallback(checkBases)
	AddCheckSnapCallback(checkEpochs)
}
