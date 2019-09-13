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
// users (and implied system group of same name) that snaps may specify. This
// will eventually be moved out of here into the store.
//
// Since the snap is mounted read-only and to avoid problems associated with
// different systems using different uids and gids for the same user name and
// group name, snapd will create system-usernames where 'scope' is not
// 'external' (currently snapd only supports 'scope: shared') with the
// following characteristics:
//
// - uid and gid shall match for the specified system-username
// - a snapd-allocated [ug]id for a user/group name shall never change
// - snapd should avoid [ug]ids that are known to overlap with uid ranges of
//   common use cases and user namespace container managers so that DAC and
//   AppArmor owner match work as intended.
// - [ug]id shall be < 2^31 to avoid (at least) broken devpts kernel code
// - [ug]id shall be >= 524288 (0x00080000) to give plenty of room for large
//   sites, default uid/gid ranges for docker (231072-296608), LXD installs
//   that setup a default /etc/sub{uid,gid} (100000-165536) and podman whose
//   tutorials reference setting up a specific default user and range
//   (100000-165536)
// - [ug]id shall be < 1,000,000 and > 1,001,000,000 (ie, 1,000,000 subordinate
//   uid with 1,000,000,000 range) to avoid overlapping with LXD's minimum and
//   maximum id ranges. LXD allows for any id range >= 65536 and doesn't
//   perform any [ug]id overlap detection with current users
// - [ug]ids assigned by snapd initially will fall within a 65536 (2^16) range
//   (see below) where the first [ug]id in the range has the 16 lower bits all
//   set to zero. This allows snapd to conveniently be bitwise aligned, follows
//   sensible conventions (see https://systemd.io/UIDS-GIDS.html) but also
//   potentially discoverable by systemd-nspawn (it assigns a different 65536
//   range to each container. Its allocation algorithm is not sequential and
//   may choose anything within its range that isn't already allocated. It's
//   detection algorithm includes (effectively) performing a getpwent()
//   operation on CANDIDATE_UID & 0XFFFF0000 and selecting another range if it
//   is assigned).
//
// What [ug]id range(s) should snapd use?
//
// While snapd does not employ user namespaces, it will operate on systems with
// container managers that do and will assign from a range of [ug]ids. It is
// desirable that snapd assigns [ug]ids that minimally conflict with the system
// and other software (potential conflicts with admin-assigned ranges in
// /etc/subuid and /etc/subgid cannot be avoided, but can be documented as well
// as detected/logged). Overlapping with container managers is non-fatal for
// snapd and the container, but introduces the possibility that a uid in the
// container matches a uid a snap is using, which is undesirable in terms of
// security (eg, DoS via ulimit, same ownership of files between container and
// snap (even if the other's files are otherwise inaccessible), etc).
//
// snapd shall assign [ug]ids from range(s) of 65536 where the lowest value in
// the range has the 16 lower bits all set to zero (initially just one range,
// but snapd can add more as needed).
//
// To avoid [ug]id overlaps, snapd shall only assign [ug]ids >= 524288
// (0x00080000) and <= 983040 (0x000F0000, ie the first 65536 range under LXD's
// minimum where the lower 16 bits are all zeroes). While [ug]ids >= 1001062400
// (0x3BAB0000, the first 65536 range above LXD's maximum where the lower 16
// bits are all zeroes) would also avoid overlap, considering nested containers
// (eg, LXD snap runs a container that runs a container that runs snapd),
// choosing >= 1001062400 would mean that the admin would need to increase the
// LXD id range for these containers for snapd to be allowed to create its
// [ug]ids in the deeply nested containers. The requirements would both be an
// administrative burden and artificially limit the number of deeply nested
// containers the host could have.
//
// Looking at the LSB and distribution defaults for login.defs, we can observe
// uids and gids in the system's initial 65536 range (ie, 0-65536):
//
// - 0-99        LSB-suggested statically assigned range (eg, root, daemon,
//               etc)
// - 0           mandatory 'root' user
// - 100-499     LSB-suggested dynamically assigned range for system users
//               (distributions often prefer a higher range, see below)
// - 500-999     typical distribution default for dynamically assigned range
//               for system users (some distributions use a smaller
//               SYS_[GU]ID_MIN)
// - 1000-60000  typical distribution default for dynamically assigned range
//               for regular users
// - 65535 (-1)  should not be assigned since '-1' might be evaluated as this
//               with set[ug]id* and chown families of functions
// - 65534 (-2)  nobody/nogroup user for NFS/etc [ug]id anonymous squashing
// - 65519-65533 systemd recommended reserved range for site-local anonymous
//               additions, etc
//
// To facilitate potential future use cases within the 65536 range snapd will
// assign from, snapd will only assign from the following subset of ranges
// relative to the range minimum (ie, its 'base' which has the lower 16 bits
// all set to zero):
//
// - 60500-60999 'scope: shared' system-usernames
// - 61000-65519 'scope: private' system-usernames
//
// Since the first [ug]id range must be >= 524288 and <= 983040 (see above) and
// following the above guide for system-usernames [ug]ids within this 65536
// range, the lowest 'scope: shared' user in this range is 584788 (0x0008EC54).
//
// Since this number is within systemd-nspawn's range of 524288-1879048191
// (0x00080000-0x6FFFFFFF), the number's lower 16 bits are not all zeroes so
// systemd-nspawn won't detect this allocation and could potentially assign the
// 65536 range starting at 0x00080000 to a container. snapd will therefore also
// create the 'snapd-range-524288-root' user and group with [ug]id 524288 to
// work within systemd-nspawn's collision detection. This user/group will not
// be assigned to snaps at this time.
//
// In short (phew!), use the following:
//
// $ snappy-debug.id-range 524288 # 0x00080000
// Host range:              524288-589823 (00080000-0008ffff; 0-65535)
// LSB static range:        524288-524387 (00080000-00080063; 0-99)
// Useradd system range:    524788-525287 (000801f4-000803e7; 500-999)
// Useradd regular range:   525288-584288 (000803e8-0008ea60; 1000-60000)
// Snapd system range:      584788-585287 (0008ec54-0008ee47; 60500-60999)
// Snapd private range:     585288-589807 (0008ee48-0008ffef; 61000-65519)
//
// Snapd is of course free to add more ranges (eg, 589824 (0x00090000)) with
// new snapd-range-<base>-root users, or to allocate differently within its
// 65536 range in the future (sequentially assigned [ug]ids are not required),
// but for now start very regimented to avoid as many problems as possible.
//
// References:
// https://forum.snapcraft.io/t/multiple-users-and-groups-in-snaps/
// https://systemd.io/UIDS-GIDS.html
// https://docs.docker.com/engine/security/userns-remap/
// https://github.com/lxc/lxd/blob/master/doc/userns-idmap.md
var supportedSystemUsernames = map[string]uint32{
	"snap_daemon": 584788,
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
	// double check that the input looks like a snapd version
	req := versionExp.FindStringSubmatch(version)
	if req == nil || req[0] != version {
		return false
	}

	if cmd.Version == "unknown" {
		return true // Development tree.
	}

	// We could (should?) use strutil.VersionCompare here and simplify
	// this code (see PR#7344). However this would change current
	// behavior, i.e. "2.41~pre1" would *not* match [snapd2.41] anymore
	// (which the code below does).
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
var osutilEnsureUserGroup = osutil.EnsureUserGroup

func validateSystemUsernames(si *snap.Info) error {
	for _, user := range si.SystemUsernames {
		if _, ok := supportedSystemUsernames[user.Name]; !ok {
			return fmt.Errorf(`snap %q requires unsupported system username "%s"`, si.InstanceName(), user.Name)
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

	// first validate
	if err := validateSystemUsernames(si); err != nil {
		return err
	}

	// then create
	// TODO: move user creation to a more appropriate place like "link-snap"
	extrausers := !release.OnClassic
	for _, user := range si.SystemUsernames {
		id := supportedSystemUsernames[user.Name]
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
