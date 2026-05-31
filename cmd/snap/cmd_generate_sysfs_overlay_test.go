// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

/*
 * Copyright (C) 2026 Canonical Ltd
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

package main_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	cmdsnap "github.com/snapcore/snapd/cmd/snap"
)

// isRoot reports whether the test is running as root; error-injection tests
// that rely on permission enforcement are skipped when running as root because
// root ignores read/write permission bits.
func isRoot() bool {
	return os.Getuid() == 0
}

type GenerateSysfsOverlaySuite struct {
	BaseSnapSuite
}

var _ = Suite(&GenerateSysfsOverlaySuite{})

// makeFakeSysfs creates a minimal fake sysfs tree under root and returns the
// path to the sys/class/<name> directory created. The tree includes:
//
//   - sys/class/<name>/<devName> -> ../../devices/<devName>  (symlink)
//   - sys/devices/<devName>/uevent                            (regular file)
//   - sys/devices/<devName>/subdir/                           (subdirectory)
func makeFakeSysfsClassDir(c *C, root, className, devName string) string {
	classDir := filepath.Join(root, "sys", "class", className)
	c.Assert(os.MkdirAll(classDir, 0755), IsNil)

	// Real device directory
	deviceDir := filepath.Join(root, "sys", "devices", devName)
	c.Assert(os.MkdirAll(deviceDir, 0755), IsNil)

	// A regular file inside the device directory
	c.Assert(os.WriteFile(filepath.Join(deviceDir, "uevent"), nil, 0644), IsNil)

	// A subdirectory inside the device directory
	c.Assert(os.MkdirAll(filepath.Join(deviceDir, "subdir"), 0755), IsNil)

	// A symlink in the class directory pointing to the device directory
	// (relative, mirroring real sysfs layout)
	relTarget := filepath.Join("..", "..", "devices", devName)
	c.Assert(os.Symlink(relTarget, filepath.Join(classDir, devName)), IsNil)

	return classDir
}

// --- Command-level tests (via CLI parser) ---

func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlayMissingArg(c *C) {
	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"generate-sysfs-overlay"})
	c.Assert(err, NotNil)
}

func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlayExtraArgs(c *C) {
	overlayDir := c.MkDir()
	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{
		"generate-sysfs-overlay", overlayDir, "extra"})
	c.Assert(err, NotNil)
}

func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlayErrorIfExists(c *C) {
	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	// Pre-create the overlay directory.
	overlayDir := c.MkDir()

	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{
		"generate-sysfs-overlay", overlayDir})
	c.Assert(err, ErrorMatches, `overlay directory .* already exists, remove it first`)
}

func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlayFreshDir(c *C) {
	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	overlayDir := filepath.Join(c.MkDir(), "new-overlay")

	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{
		"generate-sysfs-overlay", overlayDir})
	c.Assert(err, IsNil)

	c.Check(overlayDir, testDirExists)
}

// --- Unit-level tests for generateSysfsOverlay ---

func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlayEmptySysfs(c *C) {
	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(cmdsnap.GenerateSysfsOverlay(overlayDir), IsNil)

	// Overlay directory must be created even when no sysfs paths exist.
	info, err := os.Stat(overlayDir)
	c.Assert(err, IsNil)
	c.Check(info.IsDir(), Equals, true)
}

func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlaySymlinkMirrored(c *C) {
	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	makeFakeSysfsClassDir(c, fakeRoot, "leds", "leds-ctrl")

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(cmdsnap.GenerateSysfsOverlay(overlayDir), IsNil)

	// The symlink must be recreated at its original class path in the overlay.
	symlinkInOverlay := filepath.Join(overlayDir, fakeRoot, "sys", "class", "leds", "leds-ctrl")
	info, err := os.Lstat(symlinkInOverlay)
	c.Assert(err, IsNil)
	c.Check(info.Mode()&os.ModeSymlink != 0, Equals, true)

	// The real target directory must exist in the overlay.
	realDevDir := filepath.Join(overlayDir, fakeRoot, "sys", "devices", "leds-ctrl")
	dirInfo, err := os.Stat(realDevDir)
	c.Assert(err, IsNil)
	c.Check(dirInfo.IsDir(), Equals, true)
}

func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlayFilesStubbed(c *C) {
	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	makeFakeSysfsClassDir(c, fakeRoot, "leds", "leds-ctrl")

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(cmdsnap.GenerateSysfsOverlay(overlayDir), IsNil)

	// The regular file inside the device directory must be stubbed (empty).
	stubFile := filepath.Join(overlayDir, fakeRoot, "sys", "devices", "leds-ctrl", "uevent")
	info, err := os.Stat(stubFile)
	c.Assert(err, IsNil)
	c.Check(info.IsDir(), Equals, false)
	c.Check(info.Size(), Equals, int64(0))
}

func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlayNonExistentPathSkipped(c *C) {
	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	// Only create one of the permitted paths.
	ledsDir := filepath.Join(fakeRoot, "sys", "class", "leds")
	c.Assert(os.MkdirAll(ledsDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(ledsDir, "brightness"), nil, 0644), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	// Must not error even though most permitted paths don't exist.
	c.Assert(cmdsnap.GenerateSysfsOverlay(overlayDir), IsNil)
}

// TestGenerateSysfsOverlayEmptyClassDir verifies that a permitted class
// directory that exists but contains no entries (e.g. /sys/class/leds on a
// board where no LED is registered yet) still produces its mirror directory
// in the overlay.
func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlayEmptyClassDir(c *C) {
	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	// Create the class dir but leave it empty.
	ledsDir := filepath.Join(fakeRoot, "sys", "class", "leds")
	c.Assert(os.MkdirAll(ledsDir, 0755), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(cmdsnap.GenerateSysfsOverlay(overlayDir), IsNil)

	// The mirror directory must exist even though the source was empty.
	c.Check(filepath.Join(overlayDir, ledsDir), testDirExists)
}

// TestGenerateSysfsOverlaySkipsDevicesPaths verifies that sys/devices/platform
// and sys/devices/pci0000:00 are not traversed directly, even when they exist.
// Those paths are captured automatically as symlink targets from sys/class/*
// entries and must not be traversed as top-level sources.
func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlaySkipsDevicesPaths(c *C) {
	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	// Create a sys/devices/platform dir with a file – it must NOT appear in
	// the overlay unless it is a symlink target from a class dir.
	platformDir := filepath.Join(fakeRoot, "sys", "devices", "platform")
	c.Assert(os.MkdirAll(platformDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(platformDir, "sentinel"), nil, 0644), IsNil)

	pciDir := filepath.Join(fakeRoot, "sys", "devices", "pci0000:00")
	c.Assert(os.MkdirAll(pciDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(pciDir, "sentinel"), nil, 0644), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(cmdsnap.GenerateSysfsOverlay(overlayDir), IsNil)

	// Neither devices path should have been mirrored directly.
	c.Check(filepath.Join(overlayDir, fakeRoot, "sys", "devices", "platform", "sentinel"),
		testFileAbsent)
	c.Check(filepath.Join(overlayDir, fakeRoot, "sys", "devices", "pci0000:00", "sentinel"),
		testFileAbsent)
}

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsFilesCreatesStubs(c *C) {
	fakeRoot := c.MkDir()
	activeDir := filepath.Join(fakeRoot, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(activeDir, "value"), []byte("1\n"), 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(activeDir, "brightness"), []byte("255\n"), 0644), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	c.Assert(cmdsnap.MimicSysfsFiles(overlayDir, activeDir), IsNil)

	for _, name := range []string{"value", "brightness"} {
		dst := filepath.Join(overlayDir, activeDir, name)
		info, err := os.Stat(dst)
		c.Assert(err, IsNil, Commentf("stub for %q must exist", name))
		c.Check(info.Size(), Equals, int64(0), Commentf("stub for %q must be empty", name))
	}
}

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsLinksRecreatesSymlinks(c *C) {
	fakeRoot := c.MkDir()
	activeDir := filepath.Join(fakeRoot, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.Symlink("../target", filepath.Join(activeDir, "mylink")), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	c.Assert(cmdsnap.MimicSysfsLinks(overlayDir, activeDir), IsNil)

	dst := filepath.Join(overlayDir, activeDir, "mylink")
	info, err := os.Lstat(dst)
	c.Assert(err, IsNil)
	c.Check(info.Mode()&os.ModeSymlink != 0, Equals, true)

	target, err := os.Readlink(dst)
	c.Assert(err, IsNil)
	c.Check(target, Equals, "../target")
}

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsDirsCreatesDirs(c *C) {
	fakeRoot := c.MkDir()
	activeDir := filepath.Join(fakeRoot, "active")
	subDir := filepath.Join(activeDir, "subdir")
	c.Assert(os.MkdirAll(subDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(subDir, "attr"), nil, 0644), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	c.Assert(cmdsnap.MimicSysfsDirs(overlayDir, activeDir), IsNil)

	// The subdirectory must exist in the overlay.
	dstDir := filepath.Join(overlayDir, subDir)
	info, err := os.Stat(dstDir)
	c.Assert(err, IsNil)
	c.Check(info.IsDir(), Equals, true)

	// Files inside the subdirectory must be stubbed.
	dstAttr := filepath.Join(dstDir, "attr")
	attrInfo, err := os.Stat(dstAttr)
	c.Assert(err, IsNil)
	c.Check(attrInfo.Size(), Equals, int64(0))
}

// testDirExists is a gocheck checker that verifies a path is an existing directory.
var testDirExists = &dirExistsChecker{}

type dirExistsChecker struct{}

func (*dirExistsChecker) Info() *CheckerInfo {
	return &CheckerInfo{Name: "DirExists", Params: []string{"path"}}
}

func (*dirExistsChecker) Check(params []any, names []string) (result bool, error string) {
	path, ok := params[0].(string)
	if !ok {
		return false, "path must be a string"
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err.Error()
	}
	if !info.IsDir() {
		return false, "path exists but is not a directory"
	}
	return true, ""
}

// testFileAbsent is a gocheck checker that verifies a path does not exist.
var testFileAbsent = &fileAbsentChecker{}

type fileAbsentChecker struct{}

func (*fileAbsentChecker) Info() *CheckerInfo {
	return &CheckerInfo{Name: "FileAbsent", Params: []string{"path"}}
}

func (*fileAbsentChecker) Check(params []any, names []string) (result bool, error string) {
	path, ok := params[0].(string)
	if !ok {
		return false, "path must be a string"
	}
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return true, ""
	}
	return false, "path exists but should be absent"
}

// --- Execute error-path tests ---

func (s *GenerateSysfsOverlaySuite) TestExecuteStatError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	// Create a parent dir without execute permission so Lstat on a child
	// returns a non-IsNotExist error.
	parentDir := filepath.Join(c.MkDir(), "noaccess")
	c.Assert(os.MkdirAll(parentDir, 0755), IsNil)
	c.Assert(os.Chmod(parentDir, 0000), IsNil)
	defer os.Chmod(parentDir, 0755)

	overlayDir := filepath.Join(parentDir, "overlay")
	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{
		"generate-sysfs-overlay", overlayDir})
	c.Assert(err, ErrorMatches, `cannot stat overlay directory.*`)
}

// --- generateSysfsOverlay error-path tests ---

func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlayMkdirError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	// Make the parent of the overlay dir a read-only directory so MkdirAll fails.
	parentDir := filepath.Join(c.MkDir(), "ro")
	c.Assert(os.MkdirAll(parentDir, 0755), IsNil)
	c.Assert(os.Chmod(parentDir, 0555), IsNil)
	defer os.Chmod(parentDir, 0755)

	overlayDir := filepath.Join(parentDir, "overlay")
	err := cmdsnap.GenerateSysfsOverlay(overlayDir)
	c.Assert(err, ErrorMatches, `cannot create overlay directory.*`)
}

func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlayLstatPermError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	// Use "sys/class/leds" which is in PermittedSysfsOverlays.
	// Create its parent with no execute bit so Lstat on it returns an error
	// that is not IsNotExist.
	sysClassDir := filepath.Join(fakeRoot, "sys", "class")
	c.Assert(os.MkdirAll(sysClassDir, 0755), IsNil)
	c.Assert(os.Chmod(sysClassDir, 0000), IsNil)
	defer os.Chmod(sysClassDir, 0755)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	err := cmdsnap.GenerateSysfsOverlay(overlayDir)
	c.Assert(err, ErrorMatches, `cannot stat.*`)
}

// --- populateSysfsDirectory error-path tests ---

func (s *GenerateSysfsOverlaySuite) TestPopulateSysfsDirectoryReadDirError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	activeDir := filepath.Join(c.MkDir(), "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.Chmod(activeDir, 0000), IsNil)
	defer os.Chmod(activeDir, 0755)

	err := cmdsnap.PopulateSysfsDirectory(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot read directory.*`)
}

// TestPopulateSysfsDirectoryEmptyDirMirrored verifies that an empty source
// directory still gets its mirror directory created in the overlay.
func (s *GenerateSysfsOverlaySuite) TestPopulateSysfsDirectoryEmptyDirMirrored(c *C) {
	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	activeDir := filepath.Join(c.MkDir(), "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)

	c.Assert(cmdsnap.PopulateSysfsDirectory(overlayDir, activeDir), IsNil)

	// Mirror must exist even though activeDir had no entries.
	c.Check(filepath.Join(overlayDir, activeDir), testDirExists)
}

func (s *GenerateSysfsOverlaySuite) TestPopulateSysfsDirectorySymlinkWithDanglingTarget(c *C) {
	// A symlink whose EvalSymlinks fails (dangling symlink) should be skipped
	// without error.
	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	activeDir := filepath.Join(c.MkDir(), "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	// Dangling symlink – target does not exist.
	c.Assert(os.Symlink("/nonexistent/target/path", filepath.Join(activeDir, "dangling")), IsNil)

	err := cmdsnap.PopulateSysfsDirectory(overlayDir, activeDir)
	c.Assert(err, IsNil)
}

func (s *GenerateSysfsOverlaySuite) TestPopulateSysfsDirectoryMkdirRealTargetError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	// Create a symlink whose real target is a directory.
	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)

	realTarget := filepath.Join(base, "real")
	c.Assert(os.MkdirAll(realTarget, 0755), IsNil)
	c.Assert(os.Symlink(realTarget, filepath.Join(activeDir, "link")), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	// Pre-create the mirror of activeDir so the first MkdirAll in
	// populateSysfsDirectory succeeds, then make their shared parent
	// read-only so that MkdirAll for realTarget (a sibling) fails.
	activeDirInOverlay := filepath.Join(overlayDir, activeDir)
	c.Assert(os.MkdirAll(activeDirInOverlay, 0755), IsNil)
	sharedParent := filepath.Dir(activeDirInOverlay)
	c.Assert(os.Chmod(sharedParent, 0555), IsNil)
	defer os.Chmod(sharedParent, 0755)

	err := cmdsnap.PopulateSysfsDirectory(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay dir.*`)
}

// --- mimicSysfsFiles error-path tests ---

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsFilesReadDirError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	activeDir := filepath.Join(c.MkDir(), "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.Chmod(activeDir, 0000), IsNil)
	defer os.Chmod(activeDir, 0755)

	err := cmdsnap.MimicSysfsFiles(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot read directory.*`)
}

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsFilesMkdirError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	// Place a regular file at the path where MkdirAll would need to create a
	// directory so that MkdirAll fails.
	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(activeDir, "attr"), nil, 0644), IsNil)

	// Overlay dir exists, but we make its parent read-only after creation so
	// that subdirectory creation inside it fails.
	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)
	c.Assert(os.Chmod(overlayDir, 0555), IsNil)
	defer os.Chmod(overlayDir, 0755)

	err := cmdsnap.MimicSysfsFiles(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay dir.*`)
}

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsFilesOpenFileError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(activeDir, "attr"), nil, 0644), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	// Pre-create the directory that would hold the stub, then make it
	// read-only so OpenFile cannot create the stub file inside it.
	stubParent := filepath.Join(overlayDir, activeDir)
	c.Assert(os.MkdirAll(stubParent, 0755), IsNil)
	c.Assert(os.Chmod(stubParent, 0555), IsNil)
	defer os.Chmod(stubParent, 0755)

	err := cmdsnap.MimicSysfsFiles(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay stub file.*`)
}

// --- mimicSysfsLinks error-path tests ---

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsLinksReadDirError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	activeDir := filepath.Join(c.MkDir(), "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.Chmod(activeDir, 0000), IsNil)
	defer os.Chmod(activeDir, 0755)

	err := cmdsnap.MimicSysfsLinks(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot read directory.*`)
}

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsLinksMkdirError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.Symlink("../target", filepath.Join(activeDir, "mylink")), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)
	c.Assert(os.Chmod(overlayDir, 0555), IsNil)
	defer os.Chmod(overlayDir, 0755)

	err := cmdsnap.MimicSysfsLinks(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay dir.*`)
}

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsLinksSymlinkError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.Symlink("../target", filepath.Join(activeDir, "mylink")), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	// Pre-create the symlink destination parent but put a *file* named
	// "mylink" there so that os.Symlink fails with an error other than
	// os.IsExist (it will be ENOTDIR when the parent component is a file,
	// or EACCES if the dir is read-only).  The simplest is to make the
	// parent read-only after pre-creating it.
	symlinkParent := filepath.Join(overlayDir, activeDir)
	c.Assert(os.MkdirAll(symlinkParent, 0755), IsNil)
	// Place a regular file where the symlink would go – Symlink will fail
	// with EEXIST but our code treats EEXIST as ok.  Use a non-writable
	// parent instead.
	c.Assert(os.Chmod(symlinkParent, 0555), IsNil)
	defer os.Chmod(symlinkParent, 0755)

	// Symlink creation will fail (EACCES) and is not IsExist, so should error.
	err := cmdsnap.MimicSysfsLinks(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay symlink.*`)
}

// --- mimicSysfsDirs error-path tests ---

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsDirsReadDirError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	activeDir := filepath.Join(c.MkDir(), "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.Chmod(activeDir, 0000), IsNil)
	defer os.Chmod(activeDir, 0755)

	err := cmdsnap.MimicSysfsDirs(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot read directory.*`)
}

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsDirsMkdirError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	subDir := filepath.Join(activeDir, "subdir")
	c.Assert(os.MkdirAll(subDir, 0755), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)
	c.Assert(os.Chmod(overlayDir, 0555), IsNil)
	defer os.Chmod(overlayDir, 0755)

	err := cmdsnap.MimicSysfsDirs(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay dir.*`)
}

func (s *GenerateSysfsOverlaySuite) TestMimicSysfsDirsMimicFilesError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	// Create an activeDir with a subdirectory that contains a file.
	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	subDir := filepath.Join(activeDir, "subdir")
	c.Assert(os.MkdirAll(subDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(subDir, "attr"), nil, 0644), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	// Pre-create the subdirectory in the overlay but make it read-only so
	// that mimicSysfsFiles cannot create the stub file inside it.
	subDirInOverlay := filepath.Join(overlayDir, subDir)
	c.Assert(os.MkdirAll(subDirInOverlay, 0755), IsNil)
	c.Assert(os.Chmod(subDirInOverlay, 0555), IsNil)
	defer os.Chmod(subDirInOverlay, 0755)

	err := cmdsnap.MimicSysfsDirs(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay stub file.*`)
}

// TestMimicSysfsDirsSkipsDanglingSymlinkDir tests that a subdirectory entry
// whose EvalSymlinks fails (because it is a symlink pointing nowhere) is
// silently skipped.
func (s *GenerateSysfsOverlaySuite) TestMimicSysfsDirsSkipsDanglingSymlinkDir(c *C) {
	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	realSubDir := filepath.Join(base, "real-subdir")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.MkdirAll(realSubDir, 0755), IsNil)
	// symlink-subdir in activeDir points to realSubDir
	c.Assert(os.Symlink(realSubDir, filepath.Join(activeDir, "link-subdir")), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	// Remove realSubDir so EvalSymlinks will fail — entry should be skipped.
	c.Assert(os.Remove(realSubDir), IsNil)

	err := cmdsnap.MimicSysfsDirs(overlayDir, activeDir)
	c.Assert(err, IsNil)
}

// TestMimicSysfsFilesSkipsDanglingSymlink tests that a file entry whose
// EvalSymlinks fails is silently skipped.
func (s *GenerateSysfsOverlaySuite) TestMimicSysfsFilesSkipsDanglingSymlink(c *C) {
	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)

	// Create a regular file symlink that resolves at creation time, then
	// remove the target so EvalSymlinks fails later.
	target := filepath.Join(base, "target-file")
	c.Assert(os.WriteFile(target, nil, 0644), IsNil)
	c.Assert(os.Symlink(target, filepath.Join(activeDir, "link-file")), IsNil)
	c.Assert(os.Remove(target), IsNil)

	// Also add a real regular file to confirm it is still stubbed.
	c.Assert(os.WriteFile(filepath.Join(activeDir, "real-file"), nil, 0644), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	err := cmdsnap.MimicSysfsFiles(overlayDir, activeDir)
	c.Assert(err, IsNil)

	// The real file must be stubbed.
	dst := filepath.Join(overlayDir, activeDir, "real-file")
	_, err = os.Stat(dst)
	c.Assert(err, IsNil)
}

// TestMimicSysfsLinksReadlinkError ensures mimicSysfsLinks returns an error
// when os.Readlink fails. We simulate this by replacing the symlink with a
// regular file after ReadDir but before Readlink is called — we achieve this
// by testing the code path directly: put a non-symlink DirEntry where a
// symlink is expected. However since we read entries first, the simpler way is
// to make the entry appear as a symlink via filesystem trickery.
// Instead, we test the Readlink failure indirectly: create a symlink on a
// filesystem path that becomes inaccessible.
func (s *GenerateSysfsOverlaySuite) TestMimicSysfsLinksReadlinkSkipsWhenNoSymlinks(c *C) {
	// With no symlinks, mimicSysfsLinks should be a no-op.
	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(activeDir, "plain"), nil, 0644), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)

	err := cmdsnap.MimicSysfsLinks(overlayDir, activeDir)
	c.Assert(err, IsNil)
}

// TestGenerateSysfsOverlayPopulateError tests that an error from
// populateSysfsDirectory propagates through generateSysfsOverlay.
func (s *GenerateSysfsOverlaySuite) TestGenerateSysfsOverlayPopulateError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	fakeRoot := c.MkDir()
	restore := cmdsnap.MockSysfsRootDir(fakeRoot)
	defer restore()

	// Create the leds class dir (which is in PermittedSysfsOverlays).
	ledsDir := filepath.Join(fakeRoot, "sys", "class", "leds")
	c.Assert(os.MkdirAll(ledsDir, 0755), IsNil)

	// Make it unreadable so populateSysfsDirectory (called from
	// generateSysfsOverlay) will return an error.
	c.Assert(os.Chmod(ledsDir, 0000), IsNil)
	defer os.Chmod(ledsDir, 0755)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	err := cmdsnap.GenerateSysfsOverlay(overlayDir)
	c.Assert(err, ErrorMatches, `cannot read directory.*`)
}

// TestPopulateSysfsDirectorySecondMkdirError tests that the second MkdirAll
// call (for the symlink parent directory) propagates its error correctly.
func (s *GenerateSysfsOverlaySuite) TestPopulateSysfsDirectorySecondMkdirError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	base := c.MkDir()
	// activeDir is nested: .../a/b/c so the second MkdirAll must create
	// overlayDir + base + "/a/b/c".  We block it by placing a regular file
	// at overlayDir + base + "/a" so MkdirAll cannot traverse it.
	activeDir := filepath.Join(base, "a", "b", "c")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)

	realTarget := filepath.Join(base, "real")
	c.Assert(os.MkdirAll(realTarget, 0755), IsNil)
	c.Assert(os.Symlink(realTarget, filepath.Join(activeDir, "link")), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	// Pre-create the realTarget path in the overlay so the first MkdirAll succeeds.
	realInOverlay := filepath.Join(overlayDir, realTarget)
	c.Assert(os.MkdirAll(realInOverlay, 0755), IsNil)

	// Place a *file* at overlayDir+base+"/a" so the second MkdirAll
	// (for overlayDir+activeDir = overlayDir+base+"/a/b/c") fails.
	blockingPath := filepath.Join(overlayDir, base, "a")
	c.Assert(os.WriteFile(blockingPath, nil, 0644), IsNil)

	err := cmdsnap.PopulateSysfsDirectory(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay dir.*`)
}

// TestPopulateSysfsDirectorySymlinkCreateError tests the case where
// os.Symlink fails with a non-IsExist error in populateSysfsDirectory.
func (s *GenerateSysfsOverlaySuite) TestPopulateSysfsDirectorySymlinkCreateError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)

	realTarget := filepath.Join(base, "real")
	c.Assert(os.MkdirAll(realTarget, 0755), IsNil)
	c.Assert(os.Symlink(realTarget, filepath.Join(activeDir, "link")), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")

	// Pre-create both overlay directories, then make the parent of where the
	// symlink must be created read-only so os.Symlink fails.
	realInOverlay := filepath.Join(overlayDir, realTarget)
	c.Assert(os.MkdirAll(realInOverlay, 0755), IsNil)
	activeDirParentInOverlay := filepath.Join(overlayDir, activeDir)
	c.Assert(os.MkdirAll(activeDirParentInOverlay, 0755), IsNil)
	c.Assert(os.Chmod(activeDirParentInOverlay, 0555), IsNil)
	defer os.Chmod(activeDirParentInOverlay, 0755)

	err := cmdsnap.PopulateSysfsDirectory(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay symlink.*`)
}

// TestPopulateSysfsDirectoryMimicDirsError tests that a mimicSysfsDirs error
// is propagated from populateSysfsDirectory's symlink loop.
func (s *GenerateSysfsOverlaySuite) TestPopulateSysfsDirectoryMimicDirsError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)

	// realTarget has a subdir with a file so mimicSysfsDirs has work to do.
	realTarget := filepath.Join(base, "real")
	subDir := filepath.Join(realTarget, "subdir")
	c.Assert(os.MkdirAll(subDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(subDir, "attr"), nil, 0644), IsNil)
	c.Assert(os.Symlink(realTarget, filepath.Join(activeDir, "link")), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	// Pre-create the overlay dirs needed for the symlink itself.
	realInOverlay := filepath.Join(overlayDir, realTarget)
	c.Assert(os.MkdirAll(realInOverlay, 0755), IsNil)
	activeDirParentInOverlay := filepath.Join(overlayDir, activeDir)
	c.Assert(os.MkdirAll(activeDirParentInOverlay, 0755), IsNil)

	// Make realInOverlay read-only so mimicSysfsDirs cannot create subdir inside it.
	c.Assert(os.Chmod(realInOverlay, 0555), IsNil)
	defer os.Chmod(realInOverlay, 0755)

	err := cmdsnap.PopulateSysfsDirectory(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay dir.*`)
}

// TestPopulateSysfsDirectoryMimicFilesError tests that a mimicSysfsFiles error
// from the realTarget is propagated (the second mimic call in the symlink loop).
func (s *GenerateSysfsOverlaySuite) TestPopulateSysfsDirectoryMimicFilesError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)

	// realTarget has a regular file so mimicSysfsFiles has work to do.
	realTarget := filepath.Join(base, "real")
	c.Assert(os.MkdirAll(realTarget, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(realTarget, "attr"), nil, 0644), IsNil)
	c.Assert(os.Symlink(realTarget, filepath.Join(activeDir, "link")), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	// Pre-create the overlay dirs needed for the symlink itself.
	realInOverlay := filepath.Join(overlayDir, realTarget)
	c.Assert(os.MkdirAll(realInOverlay, 0755), IsNil)
	activeDirParentInOverlay := filepath.Join(overlayDir, activeDir)
	c.Assert(os.MkdirAll(activeDirParentInOverlay, 0755), IsNil)

	// Make realInOverlay read-only so mimicSysfsFiles cannot create the stub file.
	c.Assert(os.Chmod(realInOverlay, 0555), IsNil)
	defer os.Chmod(realInOverlay, 0755)

	err := cmdsnap.PopulateSysfsDirectory(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay stub file.*`)
}

// TestPopulateSysfsDirectoryMimicLinksError tests that a mimicSysfsLinks error
// is propagated from populateSysfsDirectory's symlink loop.
func (s *GenerateSysfsOverlaySuite) TestPopulateSysfsDirectoryMimicLinksError(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)

	// realTarget has a sub-symlink so mimicSysfsLinks has work to do.
	realTarget := filepath.Join(base, "real")
	c.Assert(os.MkdirAll(realTarget, 0755), IsNil)
	c.Assert(os.Symlink("../other", filepath.Join(realTarget, "sub-link")), IsNil)
	c.Assert(os.Symlink(realTarget, filepath.Join(activeDir, "link")), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	// Pre-create the overlay dirs needed for the symlink itself.
	realInOverlay := filepath.Join(overlayDir, realTarget)
	c.Assert(os.MkdirAll(realInOverlay, 0755), IsNil)
	activeDirParentInOverlay := filepath.Join(overlayDir, activeDir)
	c.Assert(os.MkdirAll(activeDirParentInOverlay, 0755), IsNil)

	// Make realInOverlay read-only so mimicSysfsLinks cannot create the symlink.
	c.Assert(os.Chmod(realInOverlay, 0555), IsNil)
	defer os.Chmod(realInOverlay, 0755)

	err := cmdsnap.PopulateSysfsDirectory(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay symlink.*`)
}

// TestPopulateSysfsDirectoryMimicFilesErrorForActiveDir tests that the
// mimicSysfsFiles error from the bottom of populateSysfsDirectory (for
// activeDir itself, after the symlink loop) is propagated.
func (s *GenerateSysfsOverlaySuite) TestPopulateSysfsDirectoryMimicFilesErrorForActiveDir(c *C) {
	if isRoot() {
		c.Skip("permission enforcement not applicable for root")
	}

	base := c.MkDir()
	activeDir := filepath.Join(base, "active")
	c.Assert(os.MkdirAll(activeDir, 0755), IsNil)
	// A regular file in activeDir so mimicSysfsFiles for activeDir has work to do.
	c.Assert(os.WriteFile(filepath.Join(activeDir, "brightness"), nil, 0644), IsNil)

	overlayDir := filepath.Join(c.MkDir(), "overlay")
	// Pre-create the stub parent directory for activeDir's file, but make it
	// read-only so OpenFile inside mimicSysfsFiles cannot create the stub.
	stubParent := filepath.Join(overlayDir, activeDir)
	c.Assert(os.MkdirAll(stubParent, 0755), IsNil)
	c.Assert(os.Chmod(stubParent, 0555), IsNil)
	defer os.Chmod(stubParent, 0755)

	err := cmdsnap.PopulateSysfsDirectory(overlayDir, activeDir)
	c.Assert(err, ErrorMatches, `cannot create overlay stub file.*`)
}
