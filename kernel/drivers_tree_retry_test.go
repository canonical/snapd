// -*- Mode: Go; indent-tabs-mode: t -*-

package kernel_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

// A failure at each persistence boundary leaves either the old completed
// marker or a pending marker. A new invocation must rebuild from its inputs,
// not trust an interrupted candidate or the old marker.
func (s *kernelDriversTestSuite) TestEnsureRetryAtReconciliationBoundaries(c *C) {
	points := []string{
		"before-in", "after-in", "before-build", "after-build",
		"before-built", "after-built", "before-firmware-swap",
		"after-firmware-swap", "before-modules-swap", "after-modules-swap",
		"before-firmware-links", "after-firmware-link", "after-firmware-unlink",
		"after-firmware-links", "before-live-sync", "after-live-sync",
		"before-final", "after-final",
	}
	depmod := testutil.MockCommand(c, "depmod", "")
	defer depmod.Restore()

	for _, point := range points {
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
		modsParent := filepath.Join(dest, "lib", "modules")
		fwParent := filepath.Join(dest, "lib", "firmware")
		rootInode, modsInode, fwInode := inodeOf(c, dest), inodeOf(c, modsParent), inodeOf(c, fwParent)
		userPath := filepath.Join(fwParent, "user.bin")
		c.Assert(os.WriteFile(userPath, []byte("user"), 0644), IsNil)
		ambiguous := filepath.Join(fwParent, "relative-user")
		c.Assert(os.Symlink("unrelated.bin", ambiguous), IsNil)
		c.Assert(os.Remove(filepath.Join(mountDir, "firmware", "blob1")), IsNil)
		c.Assert(os.WriteFile(filepath.Join(mountDir, "firmware", "extra.bin"), nil, 0644), IsNil)

		seen := false
		restore := kernel.MockDriversTreeCheckpoint(func(at string) error {
			if at == point && !seen {
				seen = true
				return errors.New("interrupted")
			}
			return nil
		})
		err := kernel.EnsureKernelDriversTree(mounts, []kernel.ModulesCompMountPoints{newComp}, dest)
		restore()
		c.Assert(seen, Equals, true, Commentf("checkpoint %s was not reached", point))
		c.Assert(err, ErrorMatches, "interrupted", Commentf("checkpoint %s", point))
		c.Check(inodeOf(c, dest), Equals, rootInode)
		c.Check(inodeOf(c, modsParent), Equals, modsInode)
		c.Check(inodeOf(c, fwParent), Equals, fwInode)
		outdated, err := kernel.DriversTreeOutdated(dest)
		c.Assert(err, IsNil)
		c.Check(outdated, Equals, point != "before-in" && point != "after-final", Commentf("checkpoint %s", point))
		c.Assert(kernel.EnsureKernelDriversTree(mounts, []kernel.ModulesCompMountPoints{newComp}, dest), IsNil, Commentf("retry after %s", point))
		c.Check(filepath.Join(modsParent, version, "updates", "old"), testutil.FileAbsent)
		c.Check(filepath.Join(modsParent, version, "updates", "new"), testutil.SymlinkTargetEquals, filepath.Join(newDir, "modules", version))
		c.Check(filepath.Join(fwParent, "updates", "old.bin"), testutil.FileAbsent)
		c.Check(filepath.Join(fwParent, "updates", "new.bin"), testutil.SymlinkTargetEquals, filepath.Join(newDir, "firmware", "new.bin"))
		c.Check(filepath.Join(fwParent, "blob1"), testutil.FileAbsent)
		c.Check(filepath.Join(fwParent, "extra.bin"), testutil.SymlinkTargetEquals, filepath.Join(mountDir, "firmware", "extra.bin"))
		c.Check(userPath, testutil.FileEquals, "user")
		c.Check(ambiguous, testutil.SymlinkTargetEquals, "unrelated.bin")
		c.Check(inodeOf(c, dest), Equals, rootInode)
		c.Check(inodeOf(c, modsParent), Equals, modsInode)
		c.Check(inodeOf(c, fwParent), Equals, fwInode)
		meta, err := kernel.ReadDriversTreeMeta(dest)
		c.Assert(err, IsNil)
		c.Check(*meta.Kernel, DeepEquals, mounts)
		c.Check(meta.Components, DeepEquals, []kernel.ModulesCompMountPoints{newComp})
		c.Check(meta.Destination, Equals, dest)
		c.Check(meta.GeneratorVersion, Equals, kernel.KernelDriversTreeGeneratorVersion())
		for _, suffix := range []string{".in", ".built"} {
			c.Check(osutil.FileExists(filepath.Join(dest, "kernel.json")+suffix), Equals, false)
		}
	}
}

func (s *kernelDriversTestSuite) TestEnsureRetryAfterNewTreeRename(c *C) {
	depmod := testutil.MockCommand(c, "depmod", "")
	defer depmod.Restore()
	for _, point := range []string{"before-in", "after-in", "after-build", "after-built", "before-install-rename", "after-install-rename", "before-final", "after-final"} {
		root := c.MkDir()
		mountDir := filepath.Join(root, "source")
		createKernelSnapFilesOnlyFw(c, mountDir)
		mounts := kernel.MountPoints{Current: mountDir, Target: filepath.Join(root, "future-mount")}
		dest := filepath.Join(root, "tree")
		seen := false
		restore := kernel.MockDriversTreeCheckpoint(func(at string) error {
			if at == point && !seen {
				seen = true
				return fmt.Errorf("interrupted at %s", point)
			}
			return nil
		})
		err := kernel.EnsureKernelDriversTree(mounts, nil, dest)
		restore()
		c.Assert(seen, Equals, true)
		c.Assert(err, ErrorMatches, "interrupted at .*", Commentf("at %s", point))
		c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
		c.Check(filepath.Join(dest, "lib", "firmware", "wifi_fw.bin"), testutil.SymlinkTargetEquals, filepath.Join(mounts.Target, "firmware", "wifi_fw.bin"))
		meta, err := kernel.ReadDriversTreeMeta(dest)
		c.Assert(err, IsNil)
		c.Check(*meta.Kernel, DeepEquals, mounts)
		c.Check(osutil.FileExists(dest+"_tmp"), Equals, false)
	}
}

func (s *kernelDriversTestSuite) TestEnsureMarkerLegacyPendingAndForward(c *C) {
	root := c.MkDir()
	mountDir := filepath.Join(root, "kernel")
	createKernelSnapFilesOnlyFw(c, mountDir)
	mounts := kernel.MountPoints{Current: mountDir, Target: mountDir}
	dest := filepath.Join(root, "tree")
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	marker := filepath.Join(dest, "kernel.json")
	c.Assert(os.WriteFile(marker, []byte(`{"generator-version":1}`), 0644), IsNil)
	outdated, err := kernel.DriversTreeOutdated(dest)
	c.Assert(err, IsNil)
	c.Check(outdated, Equals, true)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	meta, err := kernel.ReadDriversTreeMeta(dest)
	c.Assert(err, IsNil)
	c.Check(meta.Destination, Equals, dest)
	c.Check(meta.GeneratorVersion, Equals, kernel.KernelDriversTreeGeneratorVersion())

	// An interrupted update overrides the final marker even when both have
	// the same generator version. It cannot be used as completion proof.
	c.Assert(os.WriteFile(marker+".built", []byte(`{"generator-version":2}`), 0644), IsNil)
	outdated, err = kernel.DriversTreeOutdated(dest)
	c.Assert(err, IsNil)
	c.Check(outdated, Equals, true)
	c.Assert(kernel.EnsureKernelDriversTree(mounts, nil, dest), IsNil)
	c.Check(marker+".built", testutil.FileAbsent)

	forward := []byte(`{"generator-version":99}`)
	c.Assert(os.WriteFile(marker+".in", forward, 0644), IsNil)
	err = kernel.EnsureKernelDriversTree(mounts, nil, dest)
	c.Assert(err, ErrorMatches, `cannot replace kernel drivers tree .* from newer generator 99`)
	data, err := os.ReadFile(marker + ".in")
	c.Assert(err, IsNil)
	c.Check(string(data), Equals, string(forward))
	c.Assert(os.Remove(marker+".in"), IsNil)
	c.Assert(os.WriteFile(marker, forward, 0644), IsNil)
	err = kernel.EnsureKernelDriversTree(mounts, nil, dest)
	c.Assert(err, ErrorMatches, `cannot replace kernel drivers tree .* from newer generator 99`)
	c.Check(strings.Contains(err.Error(), dest), Equals, true)
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
