// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package boot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// A BootParticipant handles the boot process details for a snap involved in it.
type BootParticipant interface {
	// SetNextBoot will schedule the snap to be used in the next boot. For
	// base snaps it is up to the caller to select the right bootable base
	// (from the model assertion). It is a noop for not relevant snaps.
	// Otherwise it returns whether a reboot is required.
	SetNextBoot() (rebootRequired bool, err error)

	// Is this a trivial implementation of the interface?
	IsTrivial() bool
}

// A BootKernel handles the bootloader setup of a kernel.
type BootKernel interface {
	// RemoveKernelAssets removes the unpacked kernel/initrd for the given
	// kernel snap.
	RemoveKernelAssets() error
	// ExtractKernelAssets extracts kernel/initrd/dtb data from the given
	// kernel snap, if required, to a versioned bootloader directory so
	// that the bootloader can use it.
	ExtractKernelAssets(snap.Container) error
	// Is this a trivial implementation of the interface?
	IsTrivial() bool
}

type trivial struct{}

func (trivial) SetNextBoot() (bool, error)               { return false, nil }
func (trivial) IsTrivial() bool                          { return true }
func (trivial) RemoveKernelAssets() error                { return nil }
func (trivial) ExtractKernelAssets(snap.Container) error { return nil }

// ensure trivial is a BootParticipant
var _ BootParticipant = trivial{}

// ensure trivial is a Kernel
var _ BootKernel = trivial{}

// Device carries information about the devie model and mode that is
// relevant to boot. Note snapstate.DeviceContext implements this, and that's
// the expected use case.
type Device interface {
	RunMode() bool
	Classic() bool

	Kernel() string
	Base() string
}

// Participant figures out what the BootParticipant is for the given
// arguments, and returns it. If the snap does _not_ participate in
// the boot process, the returned object will be a NOP, so it's safe
// to call anything on it always.
//
// Currently, on classic, nothing is a boot participant (returned will
// always be NOP).
func Participant(s snap.PlaceInfo, t snap.Type, dev Device) BootParticipant {
	if applicable(s, t, dev) {
		bs, err := bootStateFor(t, dev)
		if err != nil {
			// all internal errors at this point
			panic(err)
		}
		return &coreBootParticipant{s: s, bs: bs}
	}
	return trivial{}
}

// Kernel checks that the given arguments refer to a kernel snap
// that participates in the boot process, and returns the associated
// BootKernel, or a trivial implementation otherwise.
func Kernel(s snap.PlaceInfo, t snap.Type, dev Device) BootKernel {
	if t == snap.TypeKernel && applicable(s, t, dev) {
		return &coreKernel{s: s}
	}
	return trivial{}
}

func applicable(s snap.PlaceInfo, t snap.Type, dev Device) bool {
	if dev.Classic() {
		return false
	}
	// In ephemeral modes we never need to care about updating the boot
	// config. This will be done via boot.MakeBootable().
	if !dev.RunMode() {
		return false
	}

	if t != snap.TypeOS && t != snap.TypeKernel && t != snap.TypeBase {
		// note we don't currently have anything useful to do with gadgets
		return false
	}

	switch t {
	case snap.TypeKernel:
		if s.InstanceName() != dev.Kernel() {
			// a remodel might leave you in this state
			return false
		}
	case snap.TypeBase, snap.TypeOS:
		base := dev.Base()
		if base == "" {
			base = "core"
		}
		if s.InstanceName() != base {
			return false
		}
	}

	return true
}

// bootState exposes the boot state for a type of boot snap.
type bootState interface {
	// revisions retrieves the revisions of the current snap and
	// the try snap (only the latter might not be set), and
	// whether the snap is in "trying" state.
	revisions() (snap, try_snap *NameAndRevision, trying bool, err error)

	// setNext lazily implements setting the next boot target for
	// the type's boot snap. actually committing the update
	// is done via the returned bootStateUpdate's commit.
	setNext(s snap.PlaceInfo) (rebootRequired bool, u bootStateUpdate, err error)
	// markSuccessful lazily implements marking the boot
	// successful for the type's boot snap. The actual committing
	// of the update is done via bootStateUpdate's commit, that
	// way different markSuccessful can be folded together.
	markSuccessful(bootStateUpdate) (bootStateUpdate, error)
}

// bootStateFor finds the right bootState implementation of the given
// snap type and Device, if applicable.
func bootStateFor(typ snap.Type, dev Device) (s bootState, err error) {
	if !dev.RunMode() {
		return nil, fmt.Errorf("internal error: no boot state handling for ephemeral modes")
	}
	switch typ {
	case snap.TypeOS, snap.TypeBase:
		return newBootState16(snap.TypeBase), nil
	case snap.TypeKernel:
		return newBootState16(snap.TypeKernel), nil
	default:
		return nil, fmt.Errorf("internal error: no boot state handling for snap type %q", typ)
	}
}

type InUseFunc func(name string, rev snap.Revision) bool

func fixedInUse(inUse bool) InUseFunc {
	return func(string, snap.Revision) bool {
		return inUse
	}
}

// InUse returns a checker for whether a given name/revision is used in the
// boot environment for snaps of the relevant snap type.
func InUse(typ snap.Type, dev Device) (InUseFunc, error) {
	if dev.Classic() {
		// no boot state on classic
		return fixedInUse(false), nil
	}
	if !dev.RunMode() {
		// ephemeral mode, block manipulations for now
		return fixedInUse(true), nil
	}
	switch typ {
	case snap.TypeKernel, snap.TypeBase, snap.TypeOS:
		break
	default:
		return fixedInUse(false), nil
	}
	cands := make([]*NameAndRevision, 0, 2)
	s, err := bootStateFor(typ, dev)
	if err != nil {
		return nil, err
	}
	cand, try_cand, _, err := s.revisions()
	if err != nil {
		return nil, err
	}
	cands = append(cands, cand)
	if try_cand != nil {
		cands = append(cands, try_cand)
	}

	return func(name string, rev snap.Revision) bool {
		for _, cand := range cands {
			if cand.Name == name && cand.Revision == rev {
				return true
			}
		}
		return false
	}, nil
}

var (
	ErrBootNameAndRevisionNotReady = errors.New("boot revision not yet established")
)

type NameAndRevision struct {
	Name     string
	Revision snap.Revision
}

// GetCurrentBoot returns the currently set name and revision for boot for the given
// type of snap, which can be snap.TypeBase (or snap.TypeOS), or snap.TypeKernel.
// Returns ErrBootNameAndRevisionNotReady if the values are temporarily not established.
func GetCurrentBoot(t snap.Type, dev Device) (*NameAndRevision, error) {
	s, err := bootStateFor(t, dev)
	if err != nil {
		return nil, err
	}

	snap, _, trying, err := s.revisions()
	if err != nil {
		return nil, err
	}

	if trying {
		return nil, ErrBootNameAndRevisionNotReady
	}

	return snap, nil
}

// nameAndRevnoFromSnap grabs the snap name and revision from the
// value of a boot variable. E.g., foo_2.snap -> name "foo", revno 2
func nameAndRevnoFromSnap(sn string) (*NameAndRevision, error) {
	if sn == "" {
		return nil, fmt.Errorf("boot variable unset")
	}
	idx := strings.IndexByte(sn, '_')
	if idx < 1 {
		return nil, fmt.Errorf("input %q has invalid format (not enough '_')", sn)
	}
	name := sn[:idx]
	revnoNSuffix := sn[idx+1:]
	rev, err := snap.ParseRevision(strings.TrimSuffix(revnoNSuffix, ".snap"))
	if err != nil {
		return nil, err
	}
	return &NameAndRevision{Name: name, Revision: rev}, nil
}

// bootStateUpdate carries the state for an on-going boot state update.
// At the end it can be used to commit it.
type bootStateUpdate interface {
	commit() error
}

// MarkBootSuccessful marks the current boot as successful. This means
// that snappy will consider this combination of kernel/os a valid
// target for rollback.
//
// The states that a boot goes through for UC16/18 are the following:
// - By default snap_mode is "" in which case the bootloader loads
//   two squashfs'es denoted by variables snap_core and snap_kernel.
// - On a refresh of core/kernel snapd will set snap_mode=try and
//   will also set snap_try_{core,kernel} to the core/kernel that
//   will be tried next.
// - On reboot the bootloader will inspect the snap_mode and if the
//   mode is set to "try" it will set "snap_mode=trying" and then
//   try to boot the snap_try_{core,kernel}".
// - On a successful boot snapd resets snap_mode to "" and copies
//   snap_try_{core,kernel} to snap_{core,kernel}. The snap_try_*
//   values are cleared afterwards.
// - On a failing boot the bootloader will see snap_mode=trying which
//   means snapd did not start successfully. In this case the bootloader
//   will set snap_mode="" and the system will boot with the known good
//   values from snap_{core,kernel}
func MarkBootSuccessful(dev Device) error {
	const errPrefix = "cannot mark boot successful: %s"

	var u bootStateUpdate
	for _, t := range []snap.Type{snap.TypeBase, snap.TypeKernel} {
		s, err := bootStateFor(t, dev)
		if err != nil {
			return err
		}
		u, err = s.markSuccessful(u)
		if err != nil {
			return fmt.Errorf(errPrefix, err)
		}
	}

	if u != nil {
		if err := u.commit(); err != nil {
			return fmt.Errorf(errPrefix, err)
		}
	}
	return nil
}

// BootableSet represents the boot snaps of a system to be made bootable.
type BootableSet struct {
	Base       *snap.Info
	BasePath   string
	Kernel     *snap.Info
	KernelPath string

	RecoverySystemDir string

	UnpackedGadgetDir string

	// Recover is set when making the recovery partition bootable.
	Recovery bool
}

// makeBootable16 setups the image filesystem for boot with UC16
// and UC18 models. This entails:
//  - installing the bootloader configuration from the gadget
//  - creating symlinks for boot snaps from seed to the runtime blob dir
//  - setting boot env vars pointing to the revisions of the boot snaps to use
//  - extracting kernel assets as needed by the bootloader
func makeBootable16(model *asserts.Model, rootdir string, bootWith *BootableSet) error {
	opts := &bootloader.Options{
		PrepareImageTime: true,
	}

	// install the bootloader configuration from the gadget
	if err := bootloader.InstallBootConfig(bootWith.UnpackedGadgetDir, rootdir, opts); err != nil {
		return err
	}

	// setup symlinks for kernel and boot base from the blob directory
	// to the seed snaps

	snapBlobDir := dirs.SnapBlobDirUnder(rootdir)
	if err := os.MkdirAll(snapBlobDir, 0755); err != nil {
		return err
	}

	for _, fn := range []string{bootWith.BasePath, bootWith.KernelPath} {
		dst := filepath.Join(snapBlobDir, filepath.Base(fn))
		// construct a relative symlink from the blob dir
		// to the seed snap file
		relSymlink, err := filepath.Rel(snapBlobDir, fn)
		if err != nil {
			return fmt.Errorf("cannot build symlink for boot snap: %v", err)
		}
		if err := os.Symlink(relSymlink, dst); err != nil {
			return err
		}
	}

	// Set bootvars for kernel/core snaps so the system boots and
	// does the first-time initialization. There is also no
	// mounted kernel/core/base snap, but just the blobs.
	bl, err := bootloader.Find(rootdir, opts)
	if err != nil {
		return fmt.Errorf("cannot set kernel/core boot variables: %s", err)
	}

	m := map[string]string{
		"snap_mode":       "",
		"snap_try_core":   "",
		"snap_try_kernel": "",
	}
	if model.DisplayName() != "" {
		m["snap_menuentry"] = model.DisplayName()
	}

	setBoot := func(name, fn string) {
		m[name] = filepath.Base(fn)
	}
	// base
	setBoot("snap_core", bootWith.BasePath)

	// kernel
	kernelf, err := snap.Open(bootWith.KernelPath)
	if err != nil {
		return err
	}
	if err := bl.ExtractKernelAssets(bootWith.Kernel, kernelf); err != nil {
		return err
	}
	setBoot("snap_kernel", bootWith.KernelPath)

	if err := bl.SetBootVars(m); err != nil {
		return err
	}

	return nil
}

func makeBootable20(model *asserts.Model, rootdir string, bootWith *BootableSet) error {
	// we can only make a single recovery system bootable right now
	recoverySystems, err := filepath.Glob(filepath.Join(rootdir, "systems/*"))
	if err != nil {
		return fmt.Errorf("cannot validate recovery systems: %v", err)
	}
	if len(recoverySystems) > 1 {
		return fmt.Errorf("cannot make multiple recovery systems bootable yet")
	}

	opts := &bootloader.Options{
		PrepareImageTime: true,
		// setup the recovery bootloader
		Recovery: true,
	}

	// install the bootloader configuration from the gadget
	if err := bootloader.InstallBootConfig(bootWith.UnpackedGadgetDir, rootdir, opts); err != nil {
		return err
	}

	// TODO:UC20: extract kernel for e.g. ARM

	// now install the recovery system specific boot config
	bl, err := bootloader.Find(rootdir, opts)
	if err != nil {
		return fmt.Errorf("internal error: cannot find bootloader: %v", err)
	}
	rbl, ok := bl.(bootloader.RecoveryAwareBootloader)
	if !ok {
		return fmt.Errorf("cannot use %s bootloader: does not support recovery systems", bl.Name())
	}
	kernelPath, err := filepath.Rel(rootdir, bootWith.KernelPath)
	if err != nil {
		return fmt.Errorf("cannot construct kernel boot path: %v", err)
	}
	blVars := map[string]string{
		"snapd_recovery_kernel": filepath.Join("/", kernelPath),
	}
	if err := rbl.SetRecoverySystemEnv(bootWith.RecoverySystemDir, blVars); err != nil {
		return fmt.Errorf("cannot set recovery system environment: %v", err)
	}

	return nil
}

func makeBootable20RunMode(model *asserts.Model, rootdir string, bootWith *BootableSet) error {
	// XXX: move to dirs ?
	runMnt := filepath.Join(rootdir, "/run/mnt/")

	// TODO:UC20:
	// - create grub.cfg instead of using the gadget one
	// - extract kernel

	// copy kernel/base into the ubuntu-data partition
	ubuntuDataMnt := filepath.Join(runMnt, "ubuntu-data")
	snapBlobDir := dirs.SnapBlobDirUnder(filepath.Join(ubuntuDataMnt, "system-data"))
	if err := os.MkdirAll(snapBlobDir, 0755); err != nil {
		return err
	}
	for _, fn := range []string{bootWith.BasePath, bootWith.KernelPath} {
		dst := filepath.Join(snapBlobDir, filepath.Base(fn))
		if err := osutil.CopyFile(fn, dst, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync); err != nil {
			return err
		}
	}

	// write modeenv on the ubuntu-data partition
	modeenv := &Modeenv{
		Mode:           "run",
		RecoverySystem: filepath.Base(bootWith.RecoverySystemDir),
		Base:           filepath.Base(bootWith.BasePath),
		Kernel:         filepath.Base(bootWith.KernelPath),
	}
	if err := modeenv.Write(filepath.Join(runMnt, "ubuntu-data", "system-data")); err != nil {
		return fmt.Errorf("cannot write modeenv: %v", err)
	}

	// get the ubuntu-boot bootloader
	opts := &bootloader.Options{
		// TODO:UC20: we use "recovery: true" here because on
		// the partition the file layout of ubuntu-boot looks
		// the same as ubuntu-seed.
		// TODO:UC20: need a better name than recovery
		Recovery: true,
	}
	bl, err := bootloader.Find(filepath.Join(runMnt, "ubuntu-boot"), opts)
	if err != nil {
		return fmt.Errorf("internal error: cannot find run system bootloader: %v", err)
	}
	// TODO:UC20: using the UC16/18 grubenv style until we have
	// a UC20 grub.cfg and corresponding snapd early boot
	// code
	blVars := map[string]string{
		"snap_mode":   "",
		"snap_kernel": filepath.Base(bootWith.KernelPath),
		"snap_core":   filepath.Base(bootWith.BasePath),
	}
	if err := bl.SetBootVars(blVars); err != nil {
		return fmt.Errorf("cannot set run system environment: %v", err)
	}
	// TODO:UC20: extract kernel here to the static UC20 name
	// check https://github.com/snapcore/snapd/pull/7913

	// LAST step: update recovery grub's grubenv to indicate that
	// we transition to run mode now
	opts = &bootloader.Options{
		// setup the recovery bootloader
		Recovery: true,
	}
	bl, err = bootloader.Find(filepath.Join(runMnt, "ubuntu-seed"), opts)
	if err != nil {
		return fmt.Errorf("internal error: cannot find bootloader: %v", err)
	}
	blVars = map[string]string{
		"snapd_recovery_mode": "run",
	}
	if err := bl.SetBootVars(blVars); err != nil {
		return fmt.Errorf("cannot set recovery system environment: %v", err)
	}

	return nil
}

// MakeBootable sets up the given bootable set and target filesystem
// such that the system can be booted.
//
// rootdir points to an image filesystem (UC 16/18), image recovery
// filesystem (UC20 at prepare-image time) or ephemeral system (UC20
// install mode).
func MakeBootable(model *asserts.Model, rootdir string, bootWith *BootableSet) error {
	if model.Grade() == asserts.ModelGradeUnset {
		return makeBootable16(model, rootdir, bootWith)
	}

	if !bootWith.Recovery {
		return makeBootable20RunMode(model, rootdir, bootWith)
	}
	return makeBootable20(model, rootdir, bootWith)
}
