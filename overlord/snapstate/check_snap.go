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
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/interfaces"
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

// supportedSystemUsers for now contains the hardcoded list of system
// users (and implied system group of same name) that snaps may specify
// https://forum.snapcraft.io/t/phase-1-of-opt-in-per-snap-users-groups-aka-the-daemon-user/10624
var supportedSystemUsers = map[string]bool{
	"daemon": true,
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

	// check system-users
	if err := checkSystemUsers(info); err != nil {
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

func checkCoreName(st *state.State, snapInfo, curInfo *snap.Info, flags Flags, deviceCtx DeviceContext) error {
	if snapInfo.Type != snap.TypeOS {
		// not a relevant check
		return nil
	}
	if curInfo != nil {
		// already one of these installed
		return nil
	}
	core, err := CoreInfo(st)
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
	kind := ""
	var currentInfo func(*state.State) (*snap.Info, error)
	switch snapInfo.Type {
	case snap.TypeGadget:
		kind = "gadget"
		currentInfo = GadgetInfo
	case snap.TypeKernel:
		kind = "kernel"
		currentInfo = KernelInfo
	default:
		// not a relevant check
		return nil
	}

	currentSnap, err := currentInfo(st)
	// in firstboot we have no gadget/kernel yet - that is ok
	// first install rules are in devicestate!
	if err == state.ErrNoState {
		return nil
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
	if snapInfo.Type != snap.TypeApp && snapInfo.Type != snap.TypeGadget {
		return nil
	}
	if snapInfo.Base == "" {
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

// FIXME: we should be using osutil.FindGid and osutil.FindUid here, but
// cannot: https://github.com/snapcore/snapd/pull/6759#issuecomment-484944730
var (
	findUid = internalFindUid
	findGid = internalFindGid
)

// FindUid returns the identifier of the given UNIX user name.
func internalFindUid(username string) (uint64, error) {
	user, err := user.Lookup(username)
	if err != nil {
		return 0, err
	}

	return strconv.ParseUint(user.Uid, 10, 64)
}

// FindGid returns the identifier of the given UNIX group name.
func internalFindGid(groupname string) (uint64, error) {
	group, err := user.LookupGroup(groupname)
	if err != nil {
		return 0, err
	}

	return strconv.ParseUint(group.Gid, 10, 64)
}

// check that the listed system users are valid
func checkSystemUsers(si *snap.Info) error {
	if len(si.SystemUsers) == 0 {
		return nil
	}

	// Run /.../snap-seccomp version-info
	path, err := cmd.InternalToolPath("snapd")
	if err != nil {
		return err
	}
	versionInfo, err := interfaces.SeccompCompilerVersionInfo(filepath.Join(filepath.Dir(path), "snap-seccomp"))
	if err != nil {
		return fmt.Errorf("Could not obtain seccomp compiler information: %v", err)
	}
	libseccompVersion, err := seccomp_compiler.GetLibseccompVersion(versionInfo)
	if err != nil {
		return err
	}

	// Parse <libseccomp version>
	tmp := strings.Split(libseccompVersion, ".")
	maj, err := strconv.Atoi(tmp[0])
	if err != nil {
		return fmt.Errorf("Could not obtain seccomp compiler information: %v", err)
	}
	min, err := strconv.Atoi(tmp[1])
	if err != nil {
		return fmt.Errorf("Could not obtain seccomp compiler information: %v", err)
	}
	// libseccomp < 2.4 has significant argument filtering bugs that we
	// cannot reliably work around with this feature.
	if maj < 2 || (maj == 2 && min < 4) {
		return fmt.Errorf(`This snap requires that snapd be compiled against libseccomp >= 2.4.`)
	}

	// Due to https://github.com/seccomp/libseccomp-golang/issues/22,
	// golang-seccomp <= 0.9.0 cannot create correct BPFs for this feature.
	// The package does not contain any version information, but we know
	// that ActLog was implemented in the library after this issue was
	// fixed, so base the decision on that. ActLog is first available in
	// 0.9.1.
	res, err := seccomp_compiler.HasGoSeccompFeature(versionInfo, "bpf-actlog")
	if err != nil {
		return err
	}
	if !res {
		return fmt.Errorf(`This snap requires that snapd be compiled against golang-seccomp >= 0.9.1`)
	}

	for _, user := range si.SystemUsers {
		if !osutil.IsValidUsername(user) || strings.HasPrefix(user, "snap") {
			return fmt.Errorf(`Invalid system user "%s"`, user)
		}

		if !supportedSystemUsers[user] {
			return fmt.Errorf(`Unsupported system user "%s"`, user)
		}

		_, uidErr := findUid(user)
		_, gidErr := findGid(user)
		if uidErr != nil || gidErr != nil {
			return fmt.Errorf(`This snap requires that the "%s" system user and group are present on the system. For example, "useradd --system --user-group --home-dir=/nonexistent --shell=/bin/false %s" could be used to create this user and group. See "man useradd" for details.`, user, user)
		}
	}
	return nil
}

func init() {
	AddCheckSnapCallback(checkCoreName)
	AddCheckSnapCallback(checkGadgetOrKernel)
	AddCheckSnapCallback(checkBases)
	AddCheckSnapCallback(checkEpochs)
}
