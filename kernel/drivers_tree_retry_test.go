// -*- Mode: Go; indent-tabs-mode: t -*-

package kernel_test

import (
	"encoding/json"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/testutil"
)

func writePendingDriversTreeMarker(c *C, dest string, mounts kernel.MountPoints, comps []kernel.ModulesCompMountPoints) {
	data, err := json.Marshal(map[string]any{
		"generator-version": kernel.KernelDriversTreeGeneratorVersion(),
		"kernel":            mounts,
		"components":        comps,
		"destination":       dest,
	})
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(filepath.Join(dest, "kernel.json.pending"), data, 0644), IsNil)
}

// Model an interrupted reconciliation: firmware/updates reflects the new
// inputs, while modules and top-level firmware still reflect the old inputs.
func (s *kernelDriversTestSuite) TestEnsureRepairsPartiallyAppliedTree(c *C) {
	depmod := testutil.MockCommand(c, "depmod", "")
	defer depmod.Restore()

	root := c.MkDir()
	mountDir := filepath.Join(root, "mounted-kernel")
	version := "6.5.4-test"
	createKernelSnapFiles(c, version, mountDir, createKernelSnapFilesOpts{})
	oldDir := filepath.Join(root, "old-component")
	newDir := filepath.Join(root, "new-component")
	createKernelModulesCompFiles(c, version, oldDir, "old")
	createKernelModulesCompFiles(c, version, newDir, "new")
	oldComp := kernel.ModulesCompMountPoints{LinkName: "old", MountPoints: kernel.MountPoints{Current: oldDir, Target: oldDir}}
	newComp := kernel.ModulesCompMountPoints{LinkName: "new", MountPoints: kernel.MountPoints{Current: newDir, Target: newDir}}
	mounts := kernel.MountPoints{Current: mountDir, Target: mountDir}
	dest := filepath.Join(root, "tree")
	c.Assert(kernel.EnsureKernelDriversTree(mounts, []kernel.ModulesCompMountPoints{oldComp}, dest), IsNil)

	c.Assert(os.Remove(filepath.Join(mountDir, "firmware", "blob1")), IsNil)
	c.Assert(os.WriteFile(filepath.Join(mountDir, "firmware", "extra.bin"), nil, 0644), IsNil)
	fwParent := filepath.Join(dest, "lib", "firmware")
	fwUpdates := filepath.Join(fwParent, "updates")
	c.Assert(os.RemoveAll(fwUpdates), IsNil)
	c.Assert(os.Mkdir(fwUpdates, 0755), IsNil)
	c.Assert(os.Symlink(filepath.Join(newDir, "firmware", "new.bin"), filepath.Join(fwUpdates, "new.bin")), IsNil)
	writePendingDriversTreeMarker(c, dest, mounts, []kernel.ModulesCompMountPoints{newComp})
	outdated, err := kernel.DriversTreeOutdated(dest)
	c.Assert(err, IsNil)
	c.Check(outdated, Equals, true)

	c.Assert(kernel.EnsureKernelDriversTree(mounts, []kernel.ModulesCompMountPoints{newComp}, dest), IsNil)
	modsUpdates := filepath.Join(dest, "lib", "modules", version, "updates")
	c.Check(filepath.Join(modsUpdates, "old"), testutil.FileAbsent)
	c.Check(filepath.Join(modsUpdates, "new"), testutil.SymlinkTargetEquals, filepath.Join(newDir, "modules", version))
	c.Check(filepath.Join(fwUpdates, "new.bin"), testutil.SymlinkTargetEquals, filepath.Join(newDir, "firmware", "new.bin"))
	c.Check(filepath.Join(fwParent, "blob1"), testutil.FileAbsent)
	c.Check(filepath.Join(fwParent, "extra.bin"), testutil.SymlinkTargetEquals, filepath.Join(mountDir, "firmware", "extra.bin"))
	meta, err := kernel.ReadDriversTreeMeta(dest)
	c.Assert(err, IsNil)
	c.Check(meta.Components, DeepEquals, []kernel.ModulesCompMountPoints{newComp})
	c.Check(filepath.Join(dest, "kernel.json.pending"), testutil.FileAbsent)
}

func (s *kernelDriversTestSuite) TestEnsureMarkerLegacyAndForward(c *C) {
	root := c.MkDir()
	mountDir := filepath.Join(root, "kernel")
	createKernelSnapFilesOnlyFw(c, mountDir)
	mounts := kernel.MountPoints{Current: mountDir, Target: mountDir}
	dest := filepath.Join(root, "tree")
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	marker := filepath.Join(dest, "kernel.json")
	c.Assert(os.WriteFile(marker, []byte(`{"generator-version":1}`), 0644), IsNil)
	link := filepath.Join(dest, "lib", "firmware", "wifi_fw.bin")
	c.Assert(os.Remove(link), IsNil)
	c.Assert(os.Symlink("/unrelated/user.bin", link), IsNil)
	outdated, err := kernel.DriversTreeOutdated(dest)
	c.Assert(err, IsNil)
	c.Check(outdated, Equals, false)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	c.Check(link, testutil.SymlinkTargetEquals, filepath.Join(mountDir, "firmware", "wifi_fw.bin"))
	meta, err := kernel.ReadDriversTreeMeta(dest)
	c.Assert(err, IsNil)
	c.Check(meta.Destination, Equals, dest)
	c.Check(filepath.Join(dest, "kernel.json.pending"), testutil.FileAbsent)
	c.Check(meta.GeneratorVersion, Equals, kernel.KernelDriversTreeGeneratorVersion())

	forward := []byte(`{"generator-version":99}`)
	c.Assert(os.WriteFile(marker, forward, 0644), IsNil)
	writePendingDriversTreeMarker(c, dest, mounts, nil)
	outdated, err = kernel.DriversTreeOutdated(dest)
	c.Assert(err, IsNil)
	c.Check(outdated, Equals, false)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	c.Check(marker+".pending", testutil.FileAbsent)
	c.Assert(os.WriteFile(marker+".pending", forward, 0644), IsNil)
	outdated, err = kernel.DriversTreeOutdated(dest)
	c.Assert(err, IsNil)
	c.Check(outdated, Equals, true)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	c.Check(marker+".pending", testutil.FileAbsent)
	meta, err = kernel.ReadDriversTreeMeta(dest)
	c.Assert(err, IsNil)
	c.Check(meta.GeneratorVersion, Equals, kernel.KernelDriversTreeGeneratorVersion())
	c.Assert(os.WriteFile(marker, forward, 0644), IsNil)
	outdated, err = kernel.DriversTreeOutdated(dest)
	c.Assert(err, IsNil)
	c.Check(outdated, Equals, false)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	meta, err = kernel.ReadDriversTreeMeta(dest)
	c.Assert(err, IsNil)
	c.Check(meta.GeneratorVersion, Equals, kernel.KernelDriversTreeGeneratorVersion())
}

func (s *kernelDriversTestSuite) TestEnsureDiscardsCandidateWithoutLosingPending(c *C) {
	root := c.MkDir()
	source := filepath.Join(root, "kernel")
	createKernelSnapFilesOnlyFw(c, source)
	mounts := kernel.MountPoints{Current: source, Target: source}
	dest := filepath.Join(root, "tree")
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	writePendingDriversTreeMarker(c, dest, mounts, nil)
	c.Assert(os.MkdirAll(dest+"_tmp", 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dest+"_tmp", "old-candidate"), nil, 0644), IsNil)
	// Invalid sources are rejected before clearing scratch; the next valid
	// call must discard the interrupted candidate without losing pending.
	c.Assert(os.WriteFile(filepath.Join(source, "modules"), nil, 0644), IsNil)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), NotNil)
	c.Check(filepath.Join(dest+"_tmp", "old-candidate"), testutil.FilePresent)
	c.Check(filepath.Join(dest, "kernel.json.pending"), Not(testutil.FileAbsent))
	outdated, err := kernel.DriversTreeOutdated(dest)
	c.Assert(err, IsNil)
	c.Check(outdated, Equals, true)
	c.Assert(os.Remove(filepath.Join(source, "modules")), IsNil)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	c.Check(dest+"_tmp", testutil.FileAbsent)
	c.Check(filepath.Join(dest, "kernel.json.pending"), testutil.FileAbsent)
}

func (s *kernelDriversTestSuite) TestPendingReconcilesDivergentFirmwareLink(c *C) {
	root := c.MkDir()
	oldSource := filepath.Join(root, "old")
	newSource := filepath.Join(root, "new")
	createKernelSnapFilesOnlyFw(c, oldSource)
	createKernelSnapFilesOnlyFw(c, newSource)
	dest := filepath.Join(root, "tree")
	oldMounts := kernel.MountPoints{Current: oldSource, Target: oldSource}
	newMounts := kernel.MountPoints{Current: newSource, Target: newSource}
	c.Assert(kernel.EnsureKernelDriversTree(oldMounts, nil, dest), IsNil)
	writePendingDriversTreeMarker(c, dest, newMounts, nil)
	link := filepath.Join(dest, "lib", "firmware", "wifi_fw.bin")
	c.Assert(os.Remove(link), IsNil)
	unowned := filepath.Join(newSource, "firmware", "user-choice.bin")
	c.Assert(os.Symlink(unowned, link), IsNil)
	c.Assert(kernel.EnsureKernelDriversTree(newMounts, nil, dest), IsNil)
	c.Check(link, testutil.SymlinkTargetEquals, filepath.Join(newSource, "firmware", "wifi_fw.bin"))
	c.Check(filepath.Join(dest, "kernel.json.pending"), testutil.FileAbsent)
}

func (s *kernelDriversTestSuite) TestEnsurePreflightsParentBeforePending(c *C) {
	root := c.MkDir()
	source := filepath.Join(root, "kernel")
	createKernelSnapFilesOnlyFw(c, source)
	mounts := kernel.MountPoints{Current: source, Target: source}
	dest := filepath.Join(root, "tree")
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	parent := filepath.Join(dest, "lib", "modules")
	c.Assert(os.WriteFile(parent, []byte("user content"), 0644), IsNil)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), ErrorMatches, `cannot update kernel drivers tree .*parent is not a directory`)
	c.Check(filepath.Join(dest, "kernel.json.pending"), testutil.FileAbsent)
	c.Check(filepath.Join(dest, "kernel.json"), Not(testutil.FileAbsent))

	// The firmware updates directory may contain user data; a wrong type
	// must not be replaced while reconciling its children.
	c.Assert(os.Remove(parent), IsNil)
	c.Assert(os.Mkdir(parent, 0755), IsNil)
	updates := filepath.Join(dest, "lib", "firmware", "updates")
	c.Assert(os.RemoveAll(updates), IsNil)
	c.Assert(os.WriteFile(updates, []byte("user data"), 0644), IsNil)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), ErrorMatches, `cannot update kernel drivers tree .*updates.*parent is not a directory`)
	c.Check(updates, testutil.FileEquals, []byte("user data"))
	c.Check(filepath.Join(dest, "kernel.json.pending"), testutil.FileAbsent)
}

// TestEnsurePreflightsParentBeforeSourceValidation pins that the live tree's
// structural check runs before source validation (and before a candidate is
// built): its error is reported even when the source is also broken.
func (s *kernelDriversTestSuite) TestEnsurePreflightsParentBeforeSourceValidation(c *C) {
	root := c.MkDir()
	source := filepath.Join(root, "kernel")
	createKernelSnapFilesOnlyFw(c, source)
	mounts := kernel.MountPoints{Current: source, Target: source}
	dest := filepath.Join(root, "tree")
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	parent := filepath.Join(dest, "lib", "modules")
	c.Assert(os.WriteFile(parent, []byte("user content"), 0644), IsNil)
	// Make the source invalid too; the structural error must still win.
	c.Assert(os.RemoveAll(source), IsNil)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), ErrorMatches, `cannot update kernel drivers tree .*parent is not a directory`)
}

// A relocated tree must still reconcile its firmware links after first boot.
func (s *kernelDriversTestSuite) TestEnsureReconcilesAfterDestinationRelocation(c *C) {
	root := c.MkDir()
	oldSource := filepath.Join(root, "old")
	newSource := filepath.Join(root, "new")
	createKernelSnapFilesOnlyFw(c, oldSource)
	createKernelSnapFilesOnlyFw(c, newSource)
	oldMounts := kernel.MountPoints{Current: oldSource, Target: oldSource}
	newMounts := kernel.MountPoints{Current: newSource, Target: newSource}

	// Build at a staging location, as gadget/install does under
	// _writable_defaults for Ubuntu Core.
	staging := filepath.Join(root, "staging", "kernel", "pc-kernel", "1")
	c.Assert(kernel.EnsureKernelDriversTree(oldMounts, nil, staging), IsNil)

	// Simulate the relocation to the runtime root: content, including the
	// marker, moves verbatim; only the root prefix changes.
	dest := filepath.Join(root, "kernel", "pc-kernel", "1")
	c.Assert(os.MkdirAll(filepath.Dir(dest), 0755), IsNil)
	c.Assert(os.Rename(staging, dest), IsNil)
	link := filepath.Join(dest, "lib", "firmware", "wifi_fw.bin")
	c.Check(link, testutil.SymlinkTargetEquals, filepath.Join(oldSource, "firmware", "wifi_fw.bin"))

	// A later call retargets the top-level link at the runtime location.
	c.Assert(kernel.EnsureKernelDriversTree(newMounts, nil, dest), IsNil)
	c.Check(link, testutil.SymlinkTargetEquals, filepath.Join(newSource, "firmware", "wifi_fw.bin"))
}

func (s *kernelDriversTestSuite) TestEnsureIgnoresCorruptPendingButRejectsCorruptFinal(c *C) {
	root := c.MkDir()
	source := filepath.Join(root, "kernel")
	createKernelSnapFilesOnlyFw(c, source)
	mounts := kernel.MountPoints{Current: source, Target: source}
	dest := filepath.Join(root, "tree")
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dest, "kernel.json.pending"), []byte("not json"), 0644), IsNil)
	outdated, err := kernel.DriversTreeOutdated(dest)
	c.Assert(err, IsNil)
	c.Check(outdated, Equals, true)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	c.Check(filepath.Join(dest, "kernel.json.pending"), testutil.FileAbsent)

	final := filepath.Join(dest, "kernel.json")
	c.Assert(os.WriteFile(final, []byte("not json"), 0644), IsNil)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), ErrorMatches, `cannot read kernel drivers tree marker .*kernel.json.*`)
	c.Check(final, testutil.FileEquals, []byte("not json"))
}

func (s *kernelDriversTestSuite) TestEnsureMissingSourcesCannotComplete(c *C) {
	root := c.MkDir()
	mountDir := filepath.Join(root, "kernel")
	c.Assert(os.MkdirAll(mountDir, 0755), IsNil)
	dest := filepath.Join(root, "tree")
	mounts := kernel.MountPoints{Current: mountDir, Target: mountDir}
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), ErrorMatches, `cannot read kernel drivers source .*no mounted kernel content`)
	c.Check(filepath.Join(dest, "kernel.json"), testutil.FileAbsent)
	createKernelSnapFilesOnlyFw(c, mountDir)
	cmpDir := filepath.Join(root, "unmounted-component")
	c.Assert(os.MkdirAll(cmpDir, 0755), IsNil)
	cmp := kernel.ModulesCompMountPoints{LinkName: "missing", MountPoints: kernel.MountPoints{Current: cmpDir, Target: cmpDir}}
	c.Assert(kernel.EnsureKernelDriversTree(mounts, []kernel.ModulesCompMountPoints{cmp}, dest), ErrorMatches, `cannot read kernel component source .*no mounted component content`)
	c.Check(filepath.Join(dest, "kernel.json"), testutil.FileAbsent)
}
