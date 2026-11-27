// -*- Mode: Go; indent-tabs-mode: t -*-

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

package preseed_test

import (
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/image/preseed"
	"github.com/snapcore/snapd/testutil"
)

type PreseedHintsLinuxSuite struct {
	testutil.BaseTest

	root      string
	hintsFile string
}

var _ = Suite(&PreseedHintsLinuxSuite{})

func (s *PreseedHintsLinuxSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.root = c.MkDir()
	s.AddCleanup(preseed.MockSysfsRootDir(s.root))
	s.hintsFile = filepath.Join(c.MkDir(), "hints.json")
}

// mkdirIn creates the directory relPath under the fake sysfs root.
func (s *PreseedHintsLinuxSuite) mkdirIn(c *C, relPath string) string {
	p := filepath.Join(s.root, relPath)
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	return p
}

// mkfileIn creates a non-empty file at relPath under the fake sysfs root, to
// show that only the existence of the entry is captured, never its content.
func (s *PreseedHintsLinuxSuite) mkfileIn(c *C, relPath string) string {
	p := filepath.Join(s.root, relPath)
	c.Assert(os.MkdirAll(filepath.Dir(p), 0755), IsNil)
	c.Assert(os.WriteFile(p, []byte("device state\n"), 0644), IsNil)
	return p
}

// mklinkIn creates a symlink at relPath under the fake sysfs root pointing at
// the raw target.
func (s *PreseedHintsLinuxSuite) mklinkIn(c *C, relPath, target string) string {
	p := filepath.Join(s.root, relPath)
	c.Assert(os.MkdirAll(filepath.Dir(p), 0755), IsNil)
	c.Assert(os.Symlink(target, p), IsNil)
	return p
}

func (s *PreseedHintsLinuxSuite) hints(c *C) string {
	data, err := os.ReadFile(s.hintsFile)
	c.Assert(err, IsNil)
	return string(data)
}

// makeLedsDevice populates the fake root with one leds class link backed by a
// platform device directory, the shape a real system exposes.
func (s *PreseedHintsLinuxSuite) makeLedsDevice(c *C) {
	const device = "sys/devices/platform/leds/leds/input0::capslock"
	s.mkdirIn(c, "sys/class/leds")
	s.mkdirIn(c, device)
	s.mkfileIn(c, device+"/brightness")
	s.mkfileIn(c, device+"/power/control")
	s.mklinkIn(c, device+"/subsystem", "../../../../../class/leds")
	s.mklinkIn(c, "sys/class/leds/input0::capslock", "../../devices/platform/leds/leds/input0::capslock")
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteDeterministicJSON(c *C) {
	s.makeLedsDevice(c)

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	// intermediate directories are not listed: creating the overlay makes
	// them along the way
	c.Check(s.hints(c), Equals, `{
  "format": 1,
  "sysfs": {
    "directories": [
      "sys/class/leds",
      "sys/devices/platform/leds/leds/input0::capslock",
      "sys/devices/platform/leds/leds/input0::capslock/power"
    ],
    "files": [
      "sys/devices/platform/leds/leds/input0::capslock/brightness",
      "sys/devices/platform/leds/leds/input0::capslock/power/control"
    ],
    "symlinks": [
      {
        "path": "sys/class/leds/input0::capslock",
        "target": "../../devices/platform/leds/leds/input0::capslock"
      },
      {
        "path": "sys/devices/platform/leds/leds/input0::capslock/subsystem",
        "target": "../../../../../class/leds"
      }
    ]
  }
}
`)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteIsReproducible(c *C) {
	s.makeLedsDevice(c)
	s.mkfileIn(c, "sys/class/gpio/export")
	s.mkdirIn(c, "sys/class/rtc")

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)
	first := s.hints(c)

	c.Assert(os.Remove(s.hintsFile), IsNil)
	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)
	c.Check(s.hints(c), Equals, first)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteSortsEntries(c *C) {
	// created out of order, and across several classes
	for _, name := range []string{"input2::scrolllock", "input0::capslock", "input1::numlock"} {
		device := "sys/devices/platform/leds/leds/" + name
		s.mkdirIn(c, device)
		s.mklinkIn(c, "sys/class/leds/"+name, "../../devices/platform/leds/leds/"+name)
	}
	s.mkfileIn(c, "sys/class/pwm/export")
	s.mkfileIn(c, "sys/class/gpio/export")

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	c.Check(s.hints(c), Equals, `{
  "format": 1,
  "sysfs": {
    "directories": [
      "sys/class/gpio",
      "sys/class/leds",
      "sys/class/pwm",
      "sys/devices/platform/leds/leds/input0::capslock",
      "sys/devices/platform/leds/leds/input1::numlock",
      "sys/devices/platform/leds/leds/input2::scrolllock"
    ],
    "files": [
      "sys/class/gpio/export",
      "sys/class/pwm/export"
    ],
    "symlinks": [
      {
        "path": "sys/class/leds/input0::capslock",
        "target": "../../devices/platform/leds/leds/input0::capslock"
      },
      {
        "path": "sys/class/leds/input1::numlock",
        "target": "../../devices/platform/leds/leds/input1::numlock"
      },
      {
        "path": "sys/class/leds/input2::scrolllock",
        "target": "../../devices/platform/leds/leds/input2::scrolllock"
      }
    ]
  }
}
`)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteNothingFound(c *C) {
	// an empty root, no permitted class is present
	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	c.Check(s.hints(c), Equals, `{
  "format": 1
}
`)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteEmptyClassDirs(c *C) {
	// an empty class directory is still worth recording, interfaces look
	// at its existence
	s.mkdirIn(c, "sys/class/leds")
	s.mkdirIn(c, "sys/class/rtc")

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	c.Check(s.hints(c), Equals, `{
  "format": 1,
  "sysfs": {
    "directories": [
      "sys/class/leds",
      "sys/class/rtc"
    ]
  }
}
`)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteOnlyPermittedClasses(c *C) {
	s.mkdirIn(c, "sys/class/leds")
	// not in the permitted list
	s.mkfileIn(c, "sys/class/net/eth0/address")

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	c.Check(s.hints(c), Equals, `{
  "format": 1,
  "sysfs": {
    "directories": [
      "sys/class/leds"
    ]
  }
}
`)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteSkipsDevicesRoots(c *C) {
	// sys/devices roots are in the permitted list but are never walked on
	// their own: they are only captured behind a class link
	c.Check(preseed.PermitedSysfsOverlays, testutil.Contains, "sys/devices/platform")
	s.mkfileIn(c, "sys/devices/platform/unrelated/uevent")
	s.mkfileIn(c, "sys/devices/pci0000:00/0000:00:02.0/uevent")

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	c.Check(s.hints(c), Equals, `{
  "format": 1
}
`)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteSkipsDanglingClassLink(c *C) {
	s.mkdirIn(c, "sys/class/leds")
	s.mklinkIn(c, "sys/class/leds/gone", "../../devices/platform/leds/leds/gone")

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	// the class directory is recorded, the dangling link is not
	c.Check(s.hints(c), Equals, `{
  "format": 1,
  "sysfs": {
    "directories": [
      "sys/class/leds"
    ]
  }
}
`)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteDirectSubdirOfClassDir(c *C) {
	// a real subdirectory sitting in the class directory rather than a
	// class link, with its direct files
	s.mkfileIn(c, "sys/class/gpio/gpiochip0/base")
	s.mkfileIn(c, "sys/class/gpio/export")

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	c.Check(s.hints(c), Equals, `{
  "format": 1,
  "sysfs": {
    "directories": [
      "sys/class/gpio",
      "sys/class/gpio/gpiochip0"
    ],
    "files": [
      "sys/class/gpio/export",
      "sys/class/gpio/gpiochip0/base"
    ]
  }
}
`)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteDedupsSharedDeviceDir(c *C) {
	// two class links backed by the same device directory
	const device = "sys/devices/platform/rtc"
	s.mkfileIn(c, device+"/name")
	s.mklinkIn(c, "sys/class/rtc/rtc0", "../../devices/platform/rtc")
	s.mklinkIn(c, "sys/class/ptp/ptp0", "../../devices/platform/rtc")

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	c.Check(s.hints(c), Equals, `{
  "format": 1,
  "sysfs": {
    "directories": [
      "sys/class/ptp",
      "sys/class/rtc",
      "sys/devices/platform/rtc"
    ],
    "files": [
      "sys/devices/platform/rtc/name"
    ],
    "symlinks": [
      {
        "path": "sys/class/ptp/ptp0",
        "target": "../../devices/platform/rtc"
      },
      {
        "path": "sys/class/rtc/rtc0",
        "target": "../../devices/platform/rtc"
      }
    ]
  }
}
`)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteLogicalPathsExcludeCaptureRoot(c *C) {
	s.makeLedsDevice(c)

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	hints := s.hints(c)
	c.Check(strings.Contains(hints, s.root), Equals, false,
		Commentf("the capture root %q leaked into the hints document:\n%s", s.root, hints))
	for _, line := range strings.Split(hints, "\n") {
		c.Check(strings.Contains(line, `": "/`), Equals, false, Commentf("absolute path in %q", line))
	}
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteResolvesCaptureRootSymlinks(c *C) {
	s.makeLedsDevice(c)
	// reach the same tree through a symlinked root, as /tmp based test
	// roots and some production roots do
	linkedRoot := filepath.Join(c.MkDir(), "root-link")
	c.Assert(os.Symlink(s.root, linkedRoot), IsNil)
	s.AddCleanup(preseed.MockSysfsRootDir(linkedRoot))

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	hints := s.hints(c)
	c.Check(strings.Contains(hints, linkedRoot), Equals, false)
	c.Check(strings.Contains(hints, s.root), Equals, false)
	c.Check(hints, Matches, `(?s).*"sys/devices/platform/leds/leds/input0::capslock".*`)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteUnresolvableCaptureRoot(c *C) {
	s.AddCleanup(preseed.MockSysfsRootDir(filepath.Join(c.MkDir(), "missing")))

	err := preseed.WritePreseedHints(s.hintsFile)
	c.Check(err, ErrorMatches, `cannot resolve sysfs root ".*/missing": .*no such file or directory`)
	// no partially written file is left behind
	c.Check(s.hintsFile, testutil.FileAbsent)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteExistingDestination(c *C) {
	c.Assert(os.WriteFile(s.hintsFile, []byte("keep me"), 0644), IsNil)

	err := preseed.WritePreseedHints(s.hintsFile)
	c.Check(err, ErrorMatches, `preseed hints file ".*/hints.json" already exists, remove it first`)
	// the existing file is left untouched
	c.Check(s.hintsFile, testutil.FileEquals, "keep me")
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteDestinationInMissingDir(c *C) {
	hintsFile := filepath.Join(c.MkDir(), "missing", "hints.json")

	err := preseed.WritePreseedHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot create preseed hints file: open .*/missing/hints.json: no such file or directory`)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteClassDirReadError(c *C) {
	// a permitted class path which is not a directory
	s.mkfileIn(c, "sys/class/leds")

	err := preseed.WritePreseedHints(s.hintsFile)
	c.Check(err, ErrorMatches, `cannot read directory ".*/sys/class/leds": .*not a directory`)
	// the capture failed after the destination was claimed, nothing is
	// left behind
	c.Check(s.hintsFile, testutil.FileAbsent)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteDeviceDirReadError(c *C) {
	// the class link resolves to something which is not a directory
	s.mkfileIn(c, "sys/devices/platform/leds")
	s.mklinkIn(c, "sys/class/leds/broken", "../../devices/platform/leds")

	err := preseed.WritePreseedHints(s.hintsFile)
	c.Check(err, ErrorMatches, `cannot read directory ".*/sys/devices/platform/leds": .*not a directory`)
	c.Check(s.hintsFile, testutil.FileAbsent)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsWriteSubdirReadError(c *C) {
	// a class subdirectory whose direct files cannot be listed
	if os.Geteuid() == 0 {
		c.Skip("cannot test permission errors as root")
	}
	dir := s.mkdirIn(c, "sys/class/gpio/gpiochip0")
	c.Assert(os.Chmod(dir, 0000), IsNil)
	s.AddCleanup(func() { os.Chmod(dir, 0755) })

	err := preseed.WritePreseedHints(s.hintsFile)
	c.Check(err, ErrorMatches, `cannot read directory ".*/sys/class/gpio/gpiochip0": .*permission denied`)
	c.Check(s.hintsFile, testutil.FileAbsent)
}

func (s *PreseedHintsLinuxSuite) TestPreseedHintsRoundTrip(c *C) {
	s.makeLedsDevice(c)
	s.mkfileIn(c, "sys/class/gpio/export")
	s.mkdirIn(c, "sys/class/rtc")

	c.Assert(preseed.WritePreseedHints(s.hintsFile), IsNil)

	overlayDir, cleanup, err := preseed.CreateSysfsOverlayFromHints(s.hintsFile)
	c.Assert(err, IsNil)
	defer cleanup()

	// the overlay reproduces the captured topology, with the files as
	// empty stubs and the symlink targets verbatim
	c.Check(overlayLayout(c, overlayDir), DeepEquals, []string{
		"d sys",
		"d sys/class",
		"d sys/class/gpio",
		"f sys/class/gpio/export",
		"d sys/class/leds",
		"l sys/class/leds/input0::capslock -> ../../devices/platform/leds/leds/input0::capslock",
		"d sys/class/rtc",
		"d sys/devices",
		"d sys/devices/platform",
		"d sys/devices/platform/leds",
		"d sys/devices/platform/leds/leds",
		"d sys/devices/platform/leds/leds/input0::capslock",
		"f sys/devices/platform/leds/leds/input0::capslock/brightness",
		"d sys/devices/platform/leds/leds/input0::capslock/power",
		"f sys/devices/platform/leds/leds/input0::capslock/power/control",
		"l sys/devices/platform/leds/leds/input0::capslock/subsystem -> ../../../../../class/leds",
	})

	// and the class link resolves inside the overlay just like on the
	// captured system
	resolved, err := filepath.EvalSymlinks(filepath.Join(overlayDir, "sys/class/leds/input0::capslock"))
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, filepath.Join(overlayDir, "sys/devices/platform/leds/leds/input0::capslock"))
}
