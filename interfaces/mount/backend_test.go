// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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

package mount_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snaplock"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func Test(t *testing.T) {
	TestingT(t)
}

type backendSuite struct {
	ifacetest.BackendSuite

	iface2 *ifacetest.TestInterface
	iface3 *ifacetest.TestInterface
}

var _ = Suite(&backendSuite{})

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &mount.Backend{}
	s.BackendSuite.SetUpTest(c)

	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)

	c.Assert(os.MkdirAll(dirs.SnapMountPolicyDir, 0700), IsNil)
	c.Assert(os.MkdirAll(dirs.SnapRunNsDir, 0700), IsNil)

	// add second iface so that we actually test combining snippets
	s.iface2 = &ifacetest.TestInterface{InterfaceName: "iface2"}
	c.Assert(s.Repo.AddInterface(s.iface2), IsNil)

	s.iface3 = &ifacetest.TestInterface{InterfaceName: "iface3"}
	c.Assert(s.Repo.AddInterface(s.iface3), IsNil)

	s.AddCleanup(cgroup.MockVersion(2, nil))
}

func (s *backendSuite) TearDownTest(c *C) {
	s.BackendSuite.TearDownTest(c)
}

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecurityMount)
}

func (s *backendSuite) TestRemove(c *C) {
	appCanaryToGo := filepath.Join(dirs.SnapMountPolicyDir, "snap.hello-world.hello-world.fstab")
	err := os.WriteFile(appCanaryToGo, []byte("ni! ni! ni!"), 0644)
	c.Assert(err, IsNil)

	hookCanaryToGo := filepath.Join(dirs.SnapMountPolicyDir, "snap.hello-world.hook.configure.fstab")
	err = os.WriteFile(hookCanaryToGo, []byte("ni! ni! ni!"), 0644)
	c.Assert(err, IsNil)

	snapCanaryToGo := filepath.Join(dirs.SnapMountPolicyDir, "snap.hello-world.fstab")
	err = os.WriteFile(snapCanaryToGo, []byte("ni! ni! ni!"), 0644)
	c.Assert(err, IsNil)

	appCanaryToStay := filepath.Join(dirs.SnapMountPolicyDir, "snap.i-stay.really.fstab")
	err = os.WriteFile(appCanaryToStay, []byte("stay!"), 0644)
	c.Assert(err, IsNil)

	snapCanaryToStay := filepath.Join(dirs.SnapMountPolicyDir, "snap.i-stay.fstab")
	err = os.WriteFile(snapCanaryToStay, []byte("stay!"), 0644)
	c.Assert(err, IsNil)

	// Write the .mnt file, the logic for discarding mount namespaces uses it
	// as a canary file to look for to even attempt to run the mount discard
	// tool.
	mntFile := filepath.Join(dirs.SnapRunNsDir, "hello-world.mnt")
	err = os.WriteFile(mntFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	// Mock snap-discard-ns and allow tweak distro libexec dir so that it is used.
	cmd := testutil.MockCommand(c, "snap-discard-ns", "")
	defer cmd.Restore()
	dirs.DistroLibExecDir = cmd.BinDir()

	err = s.Backend.Remove("hello-world")
	c.Assert(err, IsNil)

	c.Assert(osutil.FileExists(snapCanaryToGo), Equals, false)
	c.Assert(osutil.FileExists(appCanaryToGo), Equals, false)
	c.Assert(osutil.FileExists(hookCanaryToGo), Equals, false)
	c.Assert(appCanaryToStay, testutil.FileEquals, "stay!")
	c.Assert(snapCanaryToStay, testutil.FileEquals, "stay!")
	c.Assert(cmd.Calls(), DeepEquals, [][]string{{"snap-discard-ns", "hello-world"}})
}

const mockSnapYaml = `name: snap-name
version: 1
apps:
    app1:
    app2:
hooks:
    configure:
        plugs: [iface-plug]
plugs:
    iface-plug:
        interface: iface
slots:
    iface-slot:
        interface: iface2
`

func (s *backendSuite) TestSetupSetsupSimple(c *C) {
	fsEntry1 := osutil.MountEntry{Name: "/src-1", Dir: "/dst-1", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry2 := osutil.MountEntry{Name: "/src-2", Dir: "/dst-2", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry3 := osutil.MountEntry{Name: "/src-3", Dir: "/dst-3", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}

	// Give the plug a permanent effect
	s.Iface.MountPermanentPlugCallback = func(spec *mount.Specification, plug *snap.PlugInfo) error {
		if err := spec.AddMountEntry(fsEntry1); err != nil {
			return err
		}
		return spec.AddUserMountEntry(fsEntry3)
	}
	// Give the slot a permanent effect
	s.iface2.MountPermanentSlotCallback = func(spec *mount.Specification, slot *snap.SlotInfo) error {
		return spec.AddMountEntry(fsEntry2)
	}

	// confinement options are irrelevant to this security backend
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", mockSnapYaml, 0)

	// ensure both security effects from iface/iface2 are combined
	// (because mount profiles are global in the whole snap)
	expected := strings.Split(fmt.Sprintf("%s\n%s\n", fsEntry1, fsEntry2), "\n")
	// and that we have the modern fstab file (global for snap)
	fn := filepath.Join(dirs.SnapMountPolicyDir, "snap.snap-name.fstab")
	content, err := os.ReadFile(fn)
	c.Assert(err, IsNil, Commentf("Expected mount profile for the whole snap"))
	got := strings.Split(string(content), "\n")
	c.Check(got, testutil.DeepUnsortedMatches, expected)

	// Check that the user-fstab file was written with the user mount
	fn = filepath.Join(dirs.SnapMountPolicyDir, "snap.snap-name.user-fstab")
	content, err = os.ReadFile(fn)
	c.Assert(err, IsNil, Commentf("Expected user mount profile for the whole snap"))
	c.Check(string(content), Equals, fsEntry3.String()+"\n")
}

func (s *backendSuite) TestSetupSetsupWithoutDir(c *C) {
	s.Iface.MountPermanentPlugCallback = func(spec *mount.Specification, plug *snap.PlugInfo) error {
		return spec.AddMountEntry(osutil.MountEntry{})
	}

	// Ensure that backend.Setup() creates the required dir on demand
	os.Remove(dirs.SnapMountPolicyDir)
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", mockSnapYaml, 0)
}

func (s *backendSuite) TestParallelInstanceSetup(c *C) {
	old := dirs.SnapDataDir
	defer func() {
		dirs.SnapDataDir = old
	}()
	dirs.SnapDataDir = "/var/snap"
	snapEntry := osutil.MountEntry{Name: "/snap/snap-name_instance", Dir: "/snap/snap-name", Type: "none", Options: []string{"rbind", osutil.XSnapdOriginOvername()}}
	dataEntry := osutil.MountEntry{Name: "/var/snap/snap-name_instance", Dir: "/var/snap/snap-name", Type: "none", Options: []string{"rbind", osutil.XSnapdOriginOvername()}}
	fsEntry1 := osutil.MountEntry{Name: "/src-1", Dir: "/dst-1", Type: "none", Options: []string{"bind", "ro"}}
	fsEntry2 := osutil.MountEntry{Name: "/src-2", Dir: "/dst-2", Type: "none", Options: []string{"bind", "ro"}}
	userFsEntry := osutil.MountEntry{Name: "/src-3", Dir: "/dst-3", Type: "none", Options: []string{"bind", "ro"}}

	// Give the plug a permanent effect
	s.Iface.MountPermanentPlugCallback = func(spec *mount.Specification, plug *snap.PlugInfo) error {
		if err := spec.AddMountEntry(fsEntry1); err != nil {
			return err
		}
		return spec.AddUserMountEntry(userFsEntry)
	}
	// Give the slot a permanent effect
	s.iface2.MountPermanentSlotCallback = func(spec *mount.Specification, slot *snap.SlotInfo) error {
		return spec.AddMountEntry(fsEntry2)
	}

	// confinement options are irrelevant to this security backend
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "snap-name_instance", mockSnapYaml, 0)

	// Check that snap fstab file contains parallel instance setup and data from interfaces
	expected := strings.Join([]string{snapEntry.String(), dataEntry.String(), fsEntry2.String(), fsEntry1.String()}, "\n") + "\n"
	fn := filepath.Join(dirs.SnapMountPolicyDir, "snap.snap-name_instance.fstab")
	c.Check(fn, testutil.FileEquals, expected)

	// Check that the user-fstab file was written with user mount only
	fn = filepath.Join(dirs.SnapMountPolicyDir, "snap.snap-name_instance.user-fstab")
	c.Check(fn, testutil.FileEquals, userFsEntry.String()+"\n")
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()
	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{
		"layouts",
		"mount-namespace",
		"per-snap-persistency",
		"per-snap-profiles",
		"per-snap-updates",
		"per-snap-user-profiles",
		"stale-base-invalidation",
		"freezer-cgroup-v1",
	})

	restore = cgroup.MockVersion(cgroup.V2, nil)
	defer restore()
	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{
		"layouts",
		"mount-namespace",
		"per-snap-persistency",
		"per-snap-profiles",
		"per-snap-updates",
		"per-snap-user-profiles",
		"stale-base-invalidation",
	})
}

func (s *backendSuite) TestSetupUpdates(c *C) {
	fsEntry1 := osutil.MountEntry{Name: "/src-1", Dir: "/dst-1", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry2 := osutil.MountEntry{Name: "/src-2", Dir: "/dst-2", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry3 := osutil.MountEntry{Name: "/src-3", Dir: "/dst-3", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}

	update := false
	// Give the plug a permanent effect
	s.Iface.MountPermanentPlugCallback = func(spec *mount.Specification, plug *snap.PlugInfo) error {
		return spec.AddMountEntry(fsEntry1)
	}
	// Give the slot a permanent effect
	s.iface2.MountPermanentSlotCallback = func(spec *mount.Specification, slot *snap.SlotInfo) error {
		if update {
			if err := spec.AddMountEntry(fsEntry3); err != nil {
				return err
			}
		}
		return spec.AddMountEntry(fsEntry2)
	}

	cmd := testutil.MockCommand(c, "snap-update-ns", "")
	defer cmd.Restore()
	dirs.DistroLibExecDir = cmd.BinDir()

	// confinement options are irrelevant to this security backend
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", mockSnapYaml, 0)

	// ensure both security effects from iface/iface2 are combined
	// (because mount profiles are global in the whole snap)
	expected := strings.Split(fmt.Sprintf("%s\n%s\n", fsEntry1, fsEntry2), "\n")
	// and that we have the modern fstab file (global for snap)
	fn := filepath.Join(dirs.SnapMountPolicyDir, "snap.snap-name.fstab")
	content, err := os.ReadFile(fn)
	c.Assert(err, IsNil, Commentf("Expected mount profile for the whole snap"))
	got := strings.Split(string(content), "\n")
	c.Check(got, testutil.DeepUnsortedMatches, expected)

	update = true
	// ensure .mnt file
	mntFile := filepath.Join(dirs.SnapRunNsDir, "snap-name.mnt")
	err = os.WriteFile(mntFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	// confinement options are irrelevant to this security backend
	s.UpdateSnap(c, snapInfo, interfaces.ConfinementOptions{}, mockSnapYaml, 1)

	// snap-update-ns was invoked
	c.Check(cmd.Calls(), DeepEquals, [][]string{{"snap-update-ns", "snap-name"}})

	// ensure both security effects from iface/iface2 are combined
	// (because mount profiles are global in the whole snap)
	expected = strings.Split(fmt.Sprintf("%s\n%s\n%s\n", fsEntry1, fsEntry2, fsEntry3), "\n")
	// and that we have the modern fstab file (global for snap)
	content, err = os.ReadFile(fn)
	c.Assert(err, IsNil, Commentf("Expected mount profile for the whole snap"))
	got = strings.Split(string(content), "\n")
	c.Check(got, testutil.DeepUnsortedMatches, expected)
}

func (s *backendSuite) TestSetupNoChangesNoUpdate(c *C) {
	fsEntry1 := osutil.MountEntry{Name: "/src-1", Dir: "/dst-1", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry2 := osutil.MountEntry{Name: "/src-2", Dir: "/dst-2", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}

	s.Iface.MountPermanentPlugCallback = func(spec *mount.Specification, plug *snap.PlugInfo) error {
		return spec.AddMountEntry(fsEntry1)
	}
	s.iface2.MountPermanentSlotCallback = func(spec *mount.Specification, slot *snap.SlotInfo) error {
		return spec.AddMountEntry(fsEntry2)
	}

	cmd := testutil.MockCommand(c, "snap-update-ns", "")
	defer cmd.Restore()
	dirs.DistroLibExecDir = cmd.BinDir()

	// confinement options are irrelevant to this security backend
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", mockSnapYaml, 0)

	fn := filepath.Join(dirs.SnapMountPolicyDir, "snap.snap-name.fstab")
	content1, err := os.ReadFile(fn)
	c.Assert(err, IsNil)
	c.Check(fn, testutil.FileContains, fsEntry1.String())
	c.Check(fn, testutil.FileContains, fsEntry2.String())

	// ensure .mnt file
	mntFile := filepath.Join(dirs.SnapRunNsDir, "snap-name.mnt")
	c.Assert(os.WriteFile(mntFile, []byte(""), 0644), IsNil)

	appSet, err := interfaces.NewSnapAppSet(snapInfo, nil)
	c.Assert(err, IsNil)

	sctx := interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}
	err = s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, sctx, s.Repo, timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	// snap-update-ns was not called
	c.Check(cmd.Calls(), HasLen, 0)

	// content is identical
	content2, err := os.ReadFile(fn)
	c.Assert(err, IsNil)
	c.Check(content1, DeepEquals, content2)
}

func (s *backendSuite) TestSetupUpdateChangedRemoved(c *C) {
	fsEntry1 := osutil.MountEntry{Name: "/src-1", Dir: "/dst-1", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry1Mod := osutil.MountEntry{Name: "/src-1a", Dir: "/dst-1a", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}

	const (
		modeStart = iota
		modeRemove
		modeUpdate
	)
	mode := modeStart
	s.Iface.MountPermanentPlugCallback = func(spec *mount.Specification, plug *snap.PlugInfo) error {
		switch mode {
		case modeStart:
			return spec.AddMountEntry(fsEntry1)
		case modeRemove:
			return nil
		case modeUpdate:
			return spec.AddMountEntry(fsEntry1Mod)
		default:
			panic("unexpected state")
		}
	}

	cmd := testutil.MockCommand(c, "snap-update-ns", "")
	defer cmd.Restore()
	dirs.DistroLibExecDir = cmd.BinDir()

	// confinement options are irrelevant to this security backend
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", mockSnapYaml, 0)

	fn := filepath.Join(dirs.SnapMountPolicyDir, "snap.snap-name.fstab")
	c.Check(fn, testutil.FileContains, fsEntry1.String())

	// ensure .mnt file
	mntFile := filepath.Join(dirs.SnapRunNsDir, "snap-name.mnt")
	c.Assert(os.WriteFile(mntFile, []byte(""), 0644), IsNil)

	appSet, err := interfaces.NewSnapAppSet(snapInfo, nil)
	c.Assert(err, IsNil)

	sctx := interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}

	doSetup := func() {
		err := s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, sctx, s.Repo, timings.New(nil).StartSpan("", ""))
		c.Assert(err, IsNil)
	}
	// no changes
	doSetup()
	c.Check(cmd.Calls(), HasLen, 0)

	// pretend we have an update
	mode = modeUpdate
	doSetup()
	c.Check(cmd.Calls(), HasLen, 1)
	cmd.ForgetCalls()

	// still same updated content
	doSetup()
	// no calls
	c.Check(cmd.Calls(), HasLen, 0)
	cmd.ForgetCalls()

	// now remove an entry
	mode = modeRemove
	doSetup()
	c.Check(cmd.Calls(), HasLen, 1)
	cmd.ForgetCalls()

	// one more for good measure
	doSetup()
	c.Check(cmd.Calls(), HasLen, 0)
}

func (s *backendSuite) TestSetupEndureUpdatesError(c *C) {
	fsEntry1 := osutil.MountEntry{Name: "/src-1", Dir: "/dst-1", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry2 := osutil.MountEntry{Name: "/src-2", Dir: "/dst-2", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry3 := osutil.MountEntry{Name: "/src-3", Dir: "/dst-3", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}

	update := false
	// Give the plug a permanent effect
	s.Iface.MountPermanentPlugCallback = func(spec *mount.Specification, plug *snap.PlugInfo) error {
		return spec.AddMountEntry(fsEntry1)
	}
	// Give the slot a permanent effect
	s.iface2.MountPermanentSlotCallback = func(spec *mount.Specification, slot *snap.SlotInfo) error {
		if update {
			if err := spec.AddMountEntry(fsEntry3); err != nil {
				return err
			}
		}
		return spec.AddMountEntry(fsEntry2)
	}

	cmdUpdNs := testutil.MockCommand(c, "snap-update-ns", "exit 1")
	defer cmdUpdNs.Restore()
	dirs.DistroLibExecDir = cmdUpdNs.BinDir()
	cmdUpdNs.Also("snap-discard-ns", "")

	// confinement options are irrelevant to this security backend
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", mockEndureSnapYaml, 0)

	update = true
	// ensure .mnt file
	mntFile := filepath.Join(dirs.SnapRunNsDir, "snap-name.mnt")
	err := os.WriteFile(mntFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	// confinement options are irrelevant to this security backend
	_, err = s.UpdateSnapMaybeErr(c, snapInfo, interfaces.ConfinementOptions{}, mockEndureSnapYaml, 1)
	c.Check(err, ErrorMatches, `cannot update mount namespace of snap "snap-name", and cannot discard it because it contains an enduring daemon:.*`)

	// snap-update-ns was invoked, snap-discard-ns wasn't
	c.Check(cmdUpdNs.Calls(), DeepEquals, [][]string{{"snap-update-ns", "snap-name"}})

	// no undo at this level
	expected := strings.Split(fmt.Sprintf("%s\n%s\n%s\n", fsEntry1, fsEntry2, fsEntry3), "\n")
	// and that we have the modern fstab file (global for snap)
	fn := filepath.Join(dirs.SnapMountPolicyDir, "snap.snap-name.fstab")
	content, err := os.ReadFile(fn)
	c.Assert(err, IsNil, Commentf("Expected mount profile for the whole snap"))
	got := strings.Split(string(content), "\n")
	c.Check(got, testutil.DeepUnsortedMatches, expected)
}

const mockEndureSnapYaml = `name: snap-name
version: 1
apps:
    svc1:
        daemon: simple
        refresh-mode: endure
    svc2:
        daemon: simple
        refresh-mode: endure
hooks:
    configure:
        plugs: [iface-plug]
plugs:
    iface-plug:
        interface: iface
slots:
    iface-slot:
        interface: iface2
`

func (s *backendSuite) TestSetupUpdatesErrorDiscardsNs(c *C) {
	fsEntry1 := osutil.MountEntry{Name: "/src-1", Dir: "/dst-1", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry2 := osutil.MountEntry{Name: "/src-2", Dir: "/dst-2", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry3 := osutil.MountEntry{Name: "/src-3", Dir: "/dst-3", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}

	update := false
	// Give the plug a permanent effect
	s.Iface.MountPermanentPlugCallback = func(spec *mount.Specification, plug *snap.PlugInfo) error {
		return spec.AddMountEntry(fsEntry1)
	}
	// Give the slot a permanent effect
	s.iface2.MountPermanentSlotCallback = func(spec *mount.Specification, slot *snap.SlotInfo) error {
		if update {
			if err := spec.AddMountEntry(fsEntry3); err != nil {
				return err
			}
		}
		return spec.AddMountEntry(fsEntry2)
	}

	cmdUpdNs := testutil.MockCommand(c, "snap-update-ns", "exit 1")
	defer cmdUpdNs.Restore()
	dirs.DistroLibExecDir = cmdUpdNs.BinDir()
	cmdUpdNs.Also("snap-discard-ns", "")

	// confinement options are irrelevant to this security backend
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", mockSnapYaml, 0)

	update = true
	// ensure .mnt file
	mntFile := filepath.Join(dirs.SnapRunNsDir, "snap-name.mnt")
	err := os.WriteFile(mntFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	// confinement options are irrelevant to this security backend
	s.UpdateSnap(c, snapInfo, interfaces.ConfinementOptions{}, mockSnapYaml, 1)

	// snap-update-ns was invoked, and then snap-discard-ns
	c.Check(cmdUpdNs.Calls(), DeepEquals, [][]string{{"snap-update-ns", "snap-name"}, {"snap-discard-ns", "snap-name"}})

	expected := strings.Split(fmt.Sprintf("%s\n%s\n%s\n", fsEntry1, fsEntry2, fsEntry3), "\n")
	// and that we have the modern fstab file (global for snap)
	fn := filepath.Join(dirs.SnapMountPolicyDir, "snap.snap-name.fstab")
	content, err := os.ReadFile(fn)
	c.Assert(err, IsNil, Commentf("Expected mount profile for the whole snap"))
	got := strings.Split(string(content), "\n")
	c.Check(got, testutil.DeepUnsortedMatches, expected)
}

const mockProducerSnapYaml = `name: producer
version: 1
slots:
    iface-slot:
        interface: iface3
`

const mockSystemSnapYaml = `name: system
version: 1
slots:
    system-slot:
        interface: iface2
`

const mockConsumerSnapYaml = `name: consumer
version: 1
plugs:
    iface-plug:
        interface: iface3

    system-iface-plug:
        interface: iface2
`

func (s *backendSuite) TestSetupDelaysIfDuringOtherUpdateAndConnectedOnPlugSideAndSupported(c *C) {
	// SetupContext indicates delaying effects is possible
	// - no effects are delayed for the producer snap (not supported)
	// - however, some effects were delayed for the consumer snap
	producerAppSet := s.AddSnap(c, "producer", mockProducerSnapYaml, 0)
	systemAppSet := s.AddSnap(c, "system", mockSystemSnapYaml, 0)
	consumerAppSet := s.AddSnap(c, "consumer", mockConsumerSnapYaml, 0)

	// consumer:iface-plug producer:iface-slot
	cr := interfaces.NewConnRef(consumerAppSet.Info().Plugs["iface-plug"],
		producerAppSet.Info().Slots["iface-slot"])
	_, err := s.Repo.Connect(cr, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// consumer:system-iface-plug system:system-slot
	cr = interfaces.NewConnRef(consumerAppSet.Info().Plugs["system-iface-plug"],
		systemAppSet.Info().Slots["system-slot"])
	_, err = s.Repo.Connect(cr, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	fsEntryIface3Plug := osutil.MountEntry{Name: "/src-plug", Dir: "/dst-plug", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntryIface3Slot := osutil.MountEntry{Name: "/src-slot", Dir: "/dst-slot", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntryIfaceSystemPlug := osutil.MountEntry{Name: "/src-system", Dir: "/dst-system", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}

	s.iface3.MountConnectedPlugCallback = func(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddMountEntry(fsEntryIface3Plug)
	}
	s.iface3.MountConnectedSlotCallback = func(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddMountEntry(fsEntryIface3Slot)
	}
	s.iface2.MountConnectedPlugCallback = func(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddMountEntry(fsEntryIfaceSystemPlug)
	}

	// ensure .mnt file so that calls to snap-update-ns would be done
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapRunNsDir, "producer.mnt"), []byte(""), 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapRunNsDir, "consumer.mnt"), []byte(""), 0644), IsNil)

	cmdUpdNs := testutil.MockCommand(c, "snap-update-ns", "exit 0")
	defer cmdUpdNs.Restore()
	dirs.DistroLibExecDir = cmdUpdNs.BinDir()

	sctx := interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOwnUpdate, CanDelayEffects: true,
		DelayEffect: func(backend interfaces.SecurityBackend, eff interfaces.DelayedSideEffect) {
			panic("unexpected call")
		},
	}
	err = s.Backend.Setup(producerAppSet, interfaces.ConfinementOptions{}, sctx, s.Repo, timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	// we have both fstab entry and a call to update the mount ns
	expected := fmt.Sprintf("%s\n", fsEntryIface3Slot)
	c.Check(filepath.Join(dirs.SnapMountPolicyDir, "snap.producer.fstab"), testutil.FileEquals, expected)
	c.Check(cmdUpdNs.Calls(), DeepEquals, [][]string{
		{"snap-update-ns", "producer"},
	})
	cmdUpdNs.ForgetCalls()

	var consumerDelayed interfaces.DelayedSideEffect
	sctx = interfaces.SetupContext{
		Reason: interfaces.SnapSetupReasonConnectedSlotProviderUpdate, CanDelayEffects: true,
		DelayEffect: func(backend interfaces.SecurityBackend, eff interfaces.DelayedSideEffect) {
			c.Check(backend, Equals, s.Backend)
			consumerDelayed = eff
		},
	}

	err = s.Backend.Setup(consumerAppSet, interfaces.ConfinementOptions{}, sctx, s.Repo,
		timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	// we only have an entry in fstab
	c.Check(filepath.Join(dirs.SnapMountPolicyDir, "snap.consumer.fstab"), testutil.FileMatches,
		fmt.Sprintf("%s\n", fsEntryIfaceSystemPlug))
	c.Check(filepath.Join(dirs.SnapMountPolicyDir, "snap.consumer.fstab"), testutil.FileMatches,
		fmt.Sprintf("%s\n", fsEntryIface3Plug))
	// we were notified of the delayed effects
	c.Check(consumerDelayed, Equals, interfaces.DelayedSideEffect{
		ID:          mount.DelayedConsumerMountNsUpdate,
		Description: "mount namespace update triggered by slot provider update",
	})
	// no calls to snap-update-ns
	c.Check(cmdUpdNs.Calls(), HasLen, 0)

	// a side check, no delayed effects
	defMntB := s.Backend.(interfaces.DelayedSideEffectsBackend)
	err = defMntB.ApplyDelayedEffects(consumerAppSet, nil, timings.New(nil).StartSpan("", ""))
	c.Check(err, IsNil)
	c.Check(cmdUpdNs.Calls(), HasLen, 0)

	// now apply the effect
	err = defMntB.ApplyDelayedEffects(consumerAppSet, []interfaces.DelayedSideEffect{consumerDelayed},
		timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	c.Check(cmdUpdNs.Calls(), DeepEquals, [][]string{
		{"snap-update-ns", "consumer"},
	})

	cmdUpdNs.ForgetCalls()

	// pretend we have duplicated effects for this snap, which could happen
	// when more than one provider scheduled an update
	err = defMntB.ApplyDelayedEffects(consumerAppSet,
		[]interfaces.DelayedSideEffect{
			consumerDelayed, consumerDelayed, consumerDelayed,
		},
		timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	c.Check(cmdUpdNs.Calls(), HasLen, 1)
}

func (s *backendSuite) TestApplyDelayedEffectsErrorCases(c *C) {
	consumerAppSet := s.AddSnap(c, "consumer", mockConsumerSnapYaml, 0)
	defMntB := s.Backend.(interfaces.DelayedSideEffectsBackend)
	err := defMntB.ApplyDelayedEffects(consumerAppSet,
		[]interfaces.DelayedSideEffect{
			{ID: mount.DelayedConsumerMountNsUpdate, Description: "sth"},
			{ID: interfaces.DelayedEffect("effect"), Description: "sth"},
		},
		timings.New(nil).StartSpan("", ""))
	c.Assert(err, ErrorMatches, `unexpected effect: "effect"`)

	err = defMntB.ApplyDelayedEffects(consumerAppSet,
		[]interfaces.DelayedSideEffect{
			{ID: interfaces.DelayedEffect("something-something-update-ns"), Description: "sth"},
		},
		timings.New(nil).StartSpan("", ""))
	c.Assert(err, ErrorMatches, `unexpected effect: "something-something-update-ns"`)
}

func (s *backendSuite) TestEffectNotDelayedIfConnectedOnPlugAndOwnUpdate(c *C) {
	// SetupContext indicates delaying effects is possible, the snap is
	// connected on the plug side, but it's running in the context of its own
	// update
	producerAppSet := s.AddSnap(c, "producer", mockProducerSnapYaml, 0)
	consumerAppSet := s.AddSnap(c, "consumer", mockConsumerSnapYaml, 0)

	// consumer:iface-plug producer:iface-slot
	cr := interfaces.NewConnRef(consumerAppSet.Info().Plugs["iface-plug"],
		producerAppSet.Info().Slots["iface-slot"])
	_, err := s.Repo.Connect(cr, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	fsEntryIface3Plug := osutil.MountEntry{Name: "/src-plug", Dir: "/dst-plug", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}

	s.iface3.MountConnectedPlugCallback = func(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddMountEntry(fsEntryIface3Plug)
	}

	// ensure .mnt file so that calls to snap-update-ns would be done
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapRunNsDir, "consumer.mnt"), []byte(""), 0644), IsNil)

	cmdUpdNs := testutil.MockCommand(c, "snap-update-ns", "exit 0")
	defer cmdUpdNs.Restore()
	dirs.DistroLibExecDir = cmdUpdNs.BinDir()

	sctx := interfaces.SetupContext{
		// our own update
		Reason: interfaces.SnapSetupReasonOwnUpdate,
		// can defer
		CanDelayEffects: true,
		DelayEffect: func(backend interfaces.SecurityBackend, eff interfaces.DelayedSideEffect) {
			panic("unexpected call")
		},
	}

	err = s.Backend.Setup(consumerAppSet, interfaces.ConfinementOptions{}, sctx, s.Repo, timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	// we only have an entry in fstab
	c.Check(filepath.Join(dirs.SnapMountPolicyDir, "snap.consumer.fstab"), testutil.FileMatches,
		fmt.Sprintf("%s\n", fsEntryIface3Plug))
	// snap-update-ns called
	c.Check(cmdUpdNs.Calls(), DeepEquals, [][]string{
		{"snap-update-ns", "consumer"},
	})
}

func (s *backendSuite) TestEffectNotDelayedIfConnectedOnSlot(c *C) {
	// SetupContext indicates delaying effects is possible, and Setup() is
	// called as a result of updating another snap connected to one of our slots
	producerAppSet := s.AddSnap(c, "producer", mockProducerSnapYaml, 0)
	consumerAppSet := s.AddSnap(c, "consumer", mockConsumerSnapYaml, 0)

	// consumer:iface-plug producer:iface-slot
	cr := interfaces.NewConnRef(consumerAppSet.Info().Plugs["iface-plug"],
		producerAppSet.Info().Slots["iface-slot"])
	_, err := s.Repo.Connect(cr, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	fsEntryIface3Plug := osutil.MountEntry{Name: "/src-plug", Dir: "/dst-plug", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}

	s.iface3.MountConnectedSlotCallback = func(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddMountEntry(fsEntryIface3Plug)
	}

	// ensure .mnt file so that calls to snap-update-ns would be done
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapRunNsDir, "producer.mnt"), []byte(""), 0644), IsNil)

	cmdUpdNs := testutil.MockCommand(c, "snap-update-ns", "exit 0")
	defer cmdUpdNs.Restore()
	dirs.DistroLibExecDir = cmdUpdNs.BinDir()

	sctx := interfaces.SetupContext{
		// our own update
		Reason: interfaces.SnapSetupReasonConnectedPlugConsumerUpdate,
		// can defer
		CanDelayEffects: true,
		DelayEffect: func(backend interfaces.SecurityBackend, eff interfaces.DelayedSideEffect) {
			panic("unexpected call")
		},
	}

	err = s.Backend.Setup(producerAppSet, interfaces.ConfinementOptions{}, sctx, s.Repo, timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	c.Check(filepath.Join(dirs.SnapMountPolicyDir, "snap.producer.fstab"), testutil.FileContains,
		fsEntryIface3Plug.String())
	// snap-update-ns called
	c.Check(cmdUpdNs.Calls(), DeepEquals, [][]string{
		{"snap-update-ns", "producer"},
	})
}

func (s *backendSuite) TestEffectNotDelayedWhenNotPossible(c *C) {
	// delaying effects is not possible, as indicated in SetupContext
	producerAppSet := s.AddSnap(c, "producer", mockProducerSnapYaml, 0)
	consumerAppSet := s.AddSnap(c, "consumer", mockConsumerSnapYaml, 0)

	// consumer:iface-plug producer:iface-slot
	cr := interfaces.NewConnRef(consumerAppSet.Info().Plugs["iface-plug"],
		producerAppSet.Info().Slots["iface-slot"])
	_, err := s.Repo.Connect(cr, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	fsEntryIface3Plug := osutil.MountEntry{Name: "/src-plug", Dir: "/dst-plug", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}

	s.iface3.MountConnectedPlugCallback = func(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddMountEntry(fsEntryIface3Plug)
	}

	// ensure .mnt file so that calls to snap-update-ns would be done
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapRunNsDir, "consumer.mnt"), []byte(""), 0644), IsNil)

	cmdUpdNs := testutil.MockCommand(c, "snap-update-ns", "exit 0")
	defer cmdUpdNs.Restore()
	dirs.DistroLibExecDir = cmdUpdNs.BinDir()

	sctx := interfaces.SetupContext{
		Reason:          interfaces.SnapSetupReasonConnectedSlotProviderUpdate,
		CanDelayEffects: false,
		DelayEffect: func(backend interfaces.SecurityBackend, eff interfaces.DelayedSideEffect) {
			panic("unexpected call")
		},
	}

	err = s.Backend.Setup(consumerAppSet, interfaces.ConfinementOptions{}, sctx, s.Repo, timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	// we only have an entry in fstab
	c.Check(filepath.Join(dirs.SnapMountPolicyDir, "snap.consumer.fstab"), testutil.FilePresent)
	// snap-update-ns was called
	c.Check(cmdUpdNs.Calls(), DeepEquals, [][]string{
		{"snap-update-ns", "consumer"},
	})
}

func (s *backendSuite) TestEffectApplyDelayedOpportunisticDiscardHappy(c *C) {
	// delaying effects is not possible, as indicated in SetupContext
	consumerAppSet := s.AddSnap(c, "consumer", mockConsumerSnapYaml, 0)

	cmdUpdNs := testutil.MockCommand(c, "snap-update-ns", "exit 0")
	defer cmdUpdNs.Restore()
	cmdDiscardNs := testutil.MockCommand(c, filepath.Join(cmdUpdNs.BinDir(), "snap-discard-ns"), "exit 0")
	defer cmdDiscardNs.Restore()
	dirs.DistroLibExecDir = cmdUpdNs.BinDir()

	consumerDelayed := interfaces.DelayedSideEffect{
		ID:          mount.DelayedConsumerMountNsUpdate,
		Description: "mount namespace update triggered by slot provider update",
	}

	// easy case first, no instances, no mnt file
	eff := []interfaces.DelayedSideEffect{consumerDelayed}
	defMntB := s.Backend.(interfaces.DelayedSideEffectsBackend)

	err := defMntB.ApplyDelayedEffects(consumerAppSet, eff, timings.New(nil).StartSpan("", ""))
	c.Check(err, IsNil)
	c.Check(cmdUpdNs.Calls(), HasLen, 0)
	c.Check(cmdDiscardNs.Calls(), HasLen, 0)

	cmdUpdNs.ForgetCalls()
	cmdDiscardNs.ForgetCalls()

	// write mnt file
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapRunNsDir, "consumer.mnt"), []byte(""), 0644), IsNil)

	err = defMntB.ApplyDelayedEffects(consumerAppSet, eff, timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	c.Check(cmdUpdNs.Calls(), HasLen, 0)
	c.Check(cmdDiscardNs.Calls(), DeepEquals, [][]string{
		{"snap-discard-ns", "--snap-already-locked", "consumer"},
	})

	cmdUpdNs.ForgetCalls()
	cmdDiscardNs.ForgetCalls()

	// mnt file, process instances
	s.mockCgroup(c, "a/b/c/snap.consumer.foo.123123123123.scope")
	err = defMntB.ApplyDelayedEffects(consumerAppSet, eff, timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	c.Check(cmdUpdNs.Calls(), DeepEquals, [][]string{
		{"snap-update-ns", "consumer"},
	})
	c.Check(cmdDiscardNs.Calls(), HasLen, 0)

	cmdUpdNs.ForgetCalls()
	cmdDiscardNs.ForgetCalls()

	c.Assert(os.RemoveAll(s.cgroupRoot()), IsNil)

	// hook instances
	s.mockCgroup(c, "a/b/c/snap.consumer.hook.foo.123123123123.scope")
	err = defMntB.ApplyDelayedEffects(consumerAppSet, eff, timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	c.Check(cmdUpdNs.Calls(), DeepEquals, [][]string{
		{"snap-update-ns", "consumer"},
	})
	c.Check(cmdDiscardNs.Calls(), HasLen, 0)

	cmdUpdNs.ForgetCalls()
	cmdDiscardNs.ForgetCalls()

	c.Assert(os.RemoveAll(s.cgroupRoot()), IsNil)

	// service instances
	s.mockCgroup(c, "a/b/c/snap.consumer.svc.service")
	err = defMntB.ApplyDelayedEffects(consumerAppSet, eff, timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	c.Check(cmdUpdNs.Calls(), DeepEquals, [][]string{
		{"snap-update-ns", "consumer"},
	})
	c.Check(cmdDiscardNs.Calls(), HasLen, 0)

	cmdUpdNs.ForgetCalls()
	cmdDiscardNs.ForgetCalls()

	c.Assert(os.RemoveAll(s.cgroupRoot()), IsNil)
}

func (s *backendSuite) TestEffectApplyDelayedOpportunisticDiscardErrFallsBack(c *C) {
	// delaying effects is not possible, as indicated in SetupContext
	consumerAppSet := s.AddSnap(c, "consumer", mockConsumerSnapYaml, 0)

	cmds := testutil.MockCommand(c, "snap-update-ns", "exit 0")
	defer cmds.Restore()
	// snap-discard-ns fails
	cmds.Also("snap-discard-ns", "exit 1")
	dirs.DistroLibExecDir = cmds.BinDir()

	consumerDelayed := interfaces.DelayedSideEffect{
		ID:          mount.DelayedConsumerMountNsUpdate,
		Description: "mount namespace update triggered by slot provider update",
	}

	// easy case first, no instances, no mnt file
	eff := []interfaces.DelayedSideEffect{consumerDelayed}
	defMntB := s.Backend.(interfaces.DelayedSideEffectsBackend)

	// write mnt file
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapRunNsDir, "consumer.mnt"), []byte(""), 0644), IsNil)

	err := defMntB.ApplyDelayedEffects(consumerAppSet, eff, timings.New(nil).StartSpan("", ""))
	c.Assert(err, IsNil)

	c.Check(cmds.Calls(), DeepEquals, [][]string{
		{"snap-discard-ns", "--snap-already-locked", "consumer"},
		{"snap-update-ns", "consumer"},
	})
}

func (s *backendSuite) TestEffectApplyDelayedOpportunisticDiscardErrLockHeldFallback(c *C) {
	// delaying effects is not possible, as indicated in SetupContext
	consumerAppSet := s.AddSnap(c, "consumer", mockConsumerSnapYaml, 0)

	cmds := testutil.MockCommand(c, "snap-update-ns", "exit 0")
	defer cmds.Restore()
	cmds.Also("snap-discard-ns", "exit 0")
	dirs.DistroLibExecDir = cmds.BinDir()

	consumerDelayed := interfaces.DelayedSideEffect{
		ID:          mount.DelayedConsumerMountNsUpdate,
		Description: "mount namespace update triggered by slot provider update",
	}

	// easy case first, no instances, no mnt file
	eff := []interfaces.DelayedSideEffect{consumerDelayed}
	defMntB := s.Backend.(interfaces.DelayedSideEffectsBackend)

	// write mnt file
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapRunNsDir, "consumer.mnt"), []byte(""), 0644), IsNil)

	// take the lock so nested locking attempts will fail
	err := snaplock.WithLock("consumer", func() error {
		err := defMntB.ApplyDelayedEffects(consumerAppSet, eff, timings.New(nil).StartSpan("", ""))
		c.Assert(err, IsNil)

		c.Check(cmds.Calls(), DeepEquals, [][]string{
			// only call to snap-update-ns
			{"snap-update-ns", "consumer"},
		})
		return err
	})
	c.Assert(err, IsNil)
}

func (s *backendSuite) cgroupRoot() string {
	return filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup")
}

func (s *backendSuite) mockCgroup(c *C, dir string) {
	path := filepath.Join(s.cgroupRoot(), dir)

	c.Assert(os.MkdirAll(path, 0755), IsNil)
	finalPath := filepath.Join(path, "cgroup.procs")
	c.Assert(os.WriteFile(finalPath, []byte("222222\n33333\n"), 0644), IsNil)
}
