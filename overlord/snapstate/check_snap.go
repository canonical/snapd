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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	seccomp_compiler "github.com/snapcore/snapd/sandbox/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/strutil"
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
	// Support for "kernel-assets" in gadget.yaml. I.e. having volume
	// content of the style $kernel:ref`
	"kernel-assets": true,
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
		return fmt.Errorf("snap %q assumes unsupported features: %s (try to refresh snapd)", si.InstanceName(), strings.Join(missing, ", "))
	}
	return nil
}

// regular expression which matches a version expressed as groups of digits
// separated with dots, with optional non-numbers afterwards
var versionExp = regexp.MustCompile(`^(?:[1-9][0-9]*)(?:\.(?:[0-9]+))*`)

func checkVersion(version string) bool {
	// double check that the input looks like a snapd version
	reqVersionNumMatch := versionExp.FindStringSubmatch(version)
	if reqVersionNumMatch == nil {
		return false
	}
	// this check ensures that no one can use an assumes like snapd2.48.3~pre2
	// or snapd2.48.5+20.10, as modifiers past the version number are not meant
	// to be relied on for snaps via assumes, however the check against the real
	// snapd version number below allows such non-numeric modifiers since real
	// snapds do have versions like that (for example debian pkg of snapd)
	if reqVersionNumMatch[0] != version {
		return false
	}

	req := strings.Split(reqVersionNumMatch[0], ".")

	if snapdtool.Version == "unknown" {
		return true // Development tree.
	}

	// We could (should?) use strutil.VersionCompare here and simplify
	// this code (see PR#7344). However this would change current
	// behavior, i.e. "2.41~pre1" would *not* match [snapd2.41] anymore
	// (which the code below does).
	curVersionNumMatch := versionExp.FindStringSubmatch(snapdtool.Version)
	if curVersionNumMatch == nil {
		return false
	}
	cur := strings.Split(curVersionNumMatch[0], ".")

	for i := range req {
		if i == len(cur) {
			// we hit the end of the elements of the current version number and have
			// more required version numbers left, so this doesn't match, if the
			// previous element was higher we would have broken out already, so the
			// only case left here is where we have version requirements that are
			// not met
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
		return fmt.Errorf("snap %q supported architectures (%s) are incompatible with this system (%s)", info.InstanceName(), strings.Join(info.Architectures, ", "), arch.DpkgArchitecture())
	}

	// check assumes
	if err := checkAssumes(info); err != nil {
		return err
	}

	// check and create system-usernames
	if err := checkAndCreateSystemUsernames(info); err != nil {
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
		err := check(st, s, curInfo, c, flags, deviceCtx)
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
type CheckSnapCallback func(st *state.State, snap, curSnap *snap.Info, snapf snap.Container, flags Flags, deviceCtx DeviceContext) error

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

func checkSnapdName(st *state.State, snapInfo, curInfo *snap.Info, _ snap.Container, flags Flags, deviceCtx DeviceContext) error {
	if snapInfo.Type() != snap.TypeSnapd {
		// not a relevant check
		return nil
	}
	if snapInfo.InstanceName() != "snapd" {
		return fmt.Errorf(`cannot install snap %q of type "snapd" with a name other than "snapd"`, snapInfo.InstanceName())
	}

	return nil
}

func checkCoreName(st *state.State, snapInfo, curInfo *snap.Info, _ snap.Container, flags Flags, deviceCtx DeviceContext) error {
	if snapInfo.Type() != snap.TypeOS {
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

func checkGadgetOrKernel(st *state.State, snapInfo, curInfo *snap.Info, snapf snap.Container, flags Flags, deviceCtx DeviceContext) error {
	typ := snapInfo.Type()
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

	currentSnap, err := infoForDeviceSnap(st, deviceCtx, whichName)
	if err == state.ErrNoState {
		// check if we are in the remodel case
		if deviceCtx != nil && deviceCtx.ForRemodeling() {
			if whichName(deviceCtx.Model()) == snapInfo.InstanceName() {
				return nil
			}
		}
		return fmt.Errorf("internal error: no state for %s snap %q", kind, snapInfo.InstanceName())
	}
	if err != nil {
		return fmt.Errorf("cannot find original %s snap: %v", kind, err)
	}

	if currentSnap.SnapID != "" && snapInfo.SnapID == "" {
		return fmt.Errorf("cannot replace signed %s snap with an unasserted one", kind)
	}

	if currentSnap.SnapID != "" && snapInfo.SnapID != "" {
		if currentSnap.SnapID == snapInfo.SnapID {
			// same snap
			return nil
		}
		return fmt.Errorf("cannot replace %s snap with a different one", kind)
	}

	if currentSnap.InstanceName() != snapInfo.InstanceName() {
		return fmt.Errorf("cannot replace %s snap with a different one", kind)
	}

	return nil
}

func checkBases(st *state.State, snapInfo, curInfo *snap.Info, _ snap.Container, flags Flags, deviceCtx DeviceContext) error {
	// check if this is relevant
	if snapInfo.Type() != snap.TypeApp && snapInfo.Type() != snap.TypeGadget {
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

func checkEpochs(_ *state.State, snapInfo, curInfo *snap.Info, _ snap.Container, _ Flags, deviceCtx DeviceContext) error {
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
// upgraded to info (i.e. that info's epoch can read snapst's epoch)
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

	return checkEpochs(nil, info, cur, nil, Flags{}, nil)
}

func earlyChecks(st *state.State, snapst *SnapState, update *snap.Info, flags Flags) (Flags, error) {
	flags, err := ensureInstallPreconditions(st, update, flags, snapst)
	if err != nil {
		return flags, err
	}

	if err := earlyEpochCheck(update, snapst); err != nil {
		return flags, err
	}
	return flags, nil
}

// check that the listed system users are valid
var osutilEnsureUserGroup = osutil.EnsureUserGroup

func validateSystemUsernames(si *snap.Info) error {
	for _, user := range si.SystemUsernames {
		systemUserName, ok := snap.SupportedSystemUsernames[user.Name]
		if !ok {
			return fmt.Errorf(`snap %q requires unsupported system username "%s"`, si.InstanceName(), user.Name)
		}

		if systemUserName.AllowedSnapIds != nil && si.SnapID != "" {
			// Only certain snaps can use this user; let's check whether ours
			// is one of these
			if !strutil.ListContains(systemUserName.AllowedSnapIds, si.SnapID) {
				return fmt.Errorf(`snap %q is not allowed to use the system user %q`,
					si.InstanceName(), user.Name)
			}
		}

		switch user.Scope {
		case "shared":
			// this is supported
			continue
		case "private", "external":
			// not supported yet
			return fmt.Errorf(`snap %q requires unsupported user scope "%s" for this version of snapd`, si.InstanceName(), user.Scope)
		default:
			return fmt.Errorf(`snap %q requires unsupported user scope "%s"`, si.InstanceName(), user.Scope)
		}
	}
	return nil
}

func checkAndCreateSystemUsernames(si *snap.Info) error {
	// No need to check support if no system-usernames
	if len(si.SystemUsernames) == 0 {
		return nil
	}

	// Run /.../snap-seccomp version-info
	vi, err := seccomp_compiler.CompilerVersionInfo(snapdtool.InternalToolPath)
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

	// first validate
	if err := validateSystemUsernames(si); err != nil {
		return err
	}

	// then create
	// TODO: move user creation to a more appropriate place like "link-snap"
	extrausers := !release.OnClassic
	for _, user := range si.SystemUsernames {
		id := snap.SupportedSystemUsernames[user.Name].Id
		switch user.Scope {
		case "shared":
			// Create the snapd-range-<base>-root user and group so
			// systemd-nspawn can avoid our range. Our ranges will always
			// be in 65536 chunks, so mask off the lower bits to obtain our
			// base (see above)
			rangeStart := id & 0xFFFF0000
			rangeName := fmt.Sprintf("snapd-range-%d-root", rangeStart)
			if err := osutilEnsureUserGroup(rangeName, rangeStart, extrausers); err != nil {
				return fmt.Errorf(`cannot ensure users for snap %q required system username "%s": %v`, si.InstanceName(), user.Name, err)
			}

			// Create the requested user and group
			if err := osutilEnsureUserGroup(user.Name, id, extrausers); err != nil {
				return fmt.Errorf(`cannot ensure users for snap %q required system username "%s": %v`, si.InstanceName(), user.Name, err)
			}
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
