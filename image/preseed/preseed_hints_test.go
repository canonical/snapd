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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/image/preseed"
	"github.com/snapcore/snapd/testutil"
)

type PreseedHintsSuite struct {
	testutil.BaseTest
}

var _ = Suite(&PreseedHintsSuite{})

// writeHints writes content as a hints file and returns its path.
func (s *PreseedHintsSuite) writeHints(c *C, content string) string {
	hintsFile := filepath.Join(c.MkDir(), "hints.json")
	c.Assert(os.WriteFile(hintsFile, []byte(content), 0644), IsNil)
	return hintsFile
}

// mockOverlayDir makes the materializer use a fixed, already existing
// directory as the overlay, and returns it.
func (s *PreseedHintsSuite) mockOverlayDir(c *C) string {
	overlayDir := filepath.Join(c.MkDir(), "overlay")
	c.Assert(os.MkdirAll(overlayDir, 0755), IsNil)
	s.AddCleanup(preseed.MockMakeOverlayTempDir(func() (string, error) {
		return overlayDir, nil
	}))
	return overlayDir
}

// overlayLayout returns one line per entry found under dir, sorted by
// filepath.WalkDir, as "d <path>", "f <path>" or "l <path> -> <target>" with
// paths relative to dir. Regular files are checked to be empty stubs.
func overlayLayout(c *C, dir string) []string {
	var layout []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			layout = append(layout, "l "+rel+" -> "+target)
		case d.IsDir():
			layout = append(layout, "d "+rel)
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			c.Check(info.Size(), Equals, int64(0), Commentf("%s is not an empty stub", rel))
			layout = append(layout, "f "+rel)
		default:
			return fmt.Errorf("unexpected entry %s of type %s", rel, d.Type())
		}
		return nil
	})
	c.Assert(err, IsNil)
	return layout
}

const sampleHints = `{
  "format": 1,
  "sysfs": {
    "directories": [
      "sys/class/leds",
      "sys/devices/platform/leds",
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
`

func (s *PreseedHintsSuite) TestPreseedHintsCreateSysfsOverlay(c *C) {
	overlayDir := s.mockOverlayDir(c)
	hintsFile := s.writeHints(c, sampleHints)

	dir, cleanup, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Assert(err, IsNil)
	c.Assert(cleanup, NotNil)
	defer cleanup()
	c.Check(dir, Equals, overlayDir)

	c.Check(overlayLayout(c, dir), DeepEquals, []string{
		"d sys",
		"d sys/class",
		"d sys/class/leds",
		"l sys/class/leds/input0::capslock -> ../../devices/platform/leds/leds/input0::capslock",
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
}

func (s *PreseedHintsSuite) TestPreseedHintsCreateSysfsOverlayNoSysfsNamespace(c *C) {
	overlayDir := s.mockOverlayDir(c)
	hintsFile := s.writeHints(c, `{"format": 1}`)

	dir, cleanup, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Assert(err, IsNil)
	defer cleanup()

	c.Check(dir, Equals, overlayDir)
	c.Check(overlayLayout(c, dir), HasLen, 0)
}

func (s *PreseedHintsSuite) TestPreseedHintsCreateSysfsOverlayEmptySysfsNamespace(c *C) {
	s.mockOverlayDir(c)
	hintsFile := s.writeHints(c, `{"format": 1, "sysfs": {}}`)

	dir, cleanup, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Assert(err, IsNil)
	defer cleanup()

	c.Check(overlayLayout(c, dir), HasLen, 0)
}

func (s *PreseedHintsSuite) TestPreseedHintsCreateSysfsOverlayCreatesMissingParents(c *C) {
	s.mockOverlayDir(c)
	// the producer always lists the parents, but the consumer does not
	// require it
	hintsFile := s.writeHints(c, `{
		"format": 1,
		"sysfs": {
			"files": ["sys/class/gpio/export"],
			"symlinks": [{"path": "sys/class/pwm/pwmchip0", "target": "../../devices/pwm"}]
		}
	}`)

	dir, cleanup, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Assert(err, IsNil)
	defer cleanup()

	c.Check(overlayLayout(c, dir), DeepEquals, []string{
		"d sys",
		"d sys/class",
		"d sys/class/gpio",
		"f sys/class/gpio/export",
		"d sys/class/pwm",
		"l sys/class/pwm/pwmchip0 -> ../../devices/pwm",
	})
}

func (s *PreseedHintsSuite) TestPreseedHintsCreateSysfsOverlayUsesTempDir(c *C) {
	hintsFile := s.writeHints(c, `{"format": 1, "sysfs": {"directories": ["sys/class/leds"]}}`)

	dir, cleanup, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Assert(err, IsNil)
	c.Check(filepath.Base(dir), Matches, "snapd-sysfs-overlay-.*")
	c.Check(filepath.Join(dir, "sys/class/leds"), testutil.FilePresent)

	cleanup()
	c.Check(dir, testutil.FileAbsent)
}

func (s *PreseedHintsSuite) TestPreseedHintsCleanupIsIdempotent(c *C) {
	overlayDir := s.mockOverlayDir(c)
	hintsFile := s.writeHints(c, `{"format": 1, "sysfs": {"directories": ["sys/class/leds"]}}`)

	_, cleanup, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Assert(err, IsNil)

	cleanup()
	c.Check(overlayDir, testutil.FileAbsent)
	// calling it again is a no-op and does not panic
	cleanup()
	c.Check(overlayDir, testutil.FileAbsent)
}

func (s *PreseedHintsSuite) TestPreseedHintsMissingFile(c *C) {
	missing := filepath.Join(c.MkDir(), "missing.json")

	_, _, err := preseed.CreateSysfsOverlayFromHints(missing)
	c.Check(err, ErrorMatches, `cannot open preseed hints file: open .*/missing.json: no such file or directory`)
}

func (s *PreseedHintsSuite) TestPreseedHintsMalformedJSON(c *C) {
	hintsFile := s.writeHints(c, `{"format": 1,`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot parse preseed hints file ".*/hints.json": unexpected EOF`)
}

func (s *PreseedHintsSuite) TestPreseedHintsNotAnObject(c *C) {
	hintsFile := s.writeHints(c, `[]`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot parse preseed hints file ".*": json: cannot unmarshal array into Go value of type .*`)
}

func (s *PreseedHintsSuite) TestPreseedHintsTrailingContent(c *C) {
	hintsFile := s.writeHints(c, `{"format": 1}{"format": 1}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot parse preseed hints file ".*": unexpected content after the hints document`)
}

func (s *PreseedHintsSuite) TestPreseedHintsTrailingWhitespaceIsFine(c *C) {
	s.mockOverlayDir(c)
	hintsFile := s.writeHints(c, "{\"format\": 1}\n\n")

	_, cleanup, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Assert(err, IsNil)
	cleanup()
}

func (s *PreseedHintsSuite) TestPreseedHintsUnknownTopLevelField(c *C) {
	hintsFile := s.writeHints(c, `{"format": 1, "apparmor": {}}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot parse preseed hints file ".*": json: unknown field "apparmor"`)
}

func (s *PreseedHintsSuite) TestPreseedHintsUnknownSysfsField(c *C) {
	hintsFile := s.writeHints(c, `{"format": 1, "sysfs": {"devices": []}}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot parse preseed hints file ".*": json: unknown field "devices"`)
}

func (s *PreseedHintsSuite) TestPreseedHintsUnknownSymlinkField(c *C) {
	hintsFile := s.writeHints(c, `{
		"format": 1,
		"sysfs": {"symlinks": [{"path": "sys/class/leds/x", "target": "y", "kind": "class"}]}
	}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot parse preseed hints file ".*": json: unknown field "kind"`)
}

func (s *PreseedHintsSuite) TestPreseedHintsMissingFormat(c *C) {
	hintsFile := s.writeHints(c, `{"sysfs": {"directories": ["sys/class/leds"]}}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot use preseed hints file ".*": missing format version`)
}

func (s *PreseedHintsSuite) TestPreseedHintsUnsupportedFormat(c *C) {
	for _, format := range []string{"2", "-1", "99"} {
		hintsFile := s.writeHints(c, `{"format": `+format+`}`)

		_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
		c.Check(err, ErrorMatches, `cannot use preseed hints file ".*": unsupported format version `+format+`, expected 1`)
	}
}

func (s *PreseedHintsSuite) TestPreseedHintsFormatIsCurrent(c *C) {
	// the format written by the producer is the one the consumer accepts
	c.Check(preseed.PreseedHintsFormat, Equals, 1)
}

func (s *PreseedHintsSuite) TestPreseedHintsInvalidPaths(c *C) {
	for _, tc := range []struct {
		path string
		err  string
	}{
		{"", `empty path`},
		{"/sys/class/leds", `invalid path "/sys/class/leds": must be relative to /`},
		{".", `invalid path ".": must name an entry`},
		{"..", `invalid path "..": must not escape the overlay`},
		{"../../etc/passwd", `invalid path "../../etc/passwd": must not escape the overlay`},
		{"sys/../../etc", `invalid path "sys/../../etc": must be clean`},
		{"sys/class/leds/", `invalid path "sys/class/leds/": must be clean`},
		{"sys//class/leds", `invalid path "sys//class/leds": must be clean`},
		{"sys/./class", `invalid path "sys/./class": must be clean`},
		{`sys\class\leds`, `invalid path "sys\\class\\leds": must not contain backslashes`},
	} {
		comment := Commentf("path %q", tc.path)
		// every list validates the same way
		for _, hints := range []string{
			`{"format": 1, "sysfs": {"directories": [%s]}}`,
			`{"format": 1, "sysfs": {"files": [%s]}}`,
			`{"format": 1, "sysfs": {"symlinks": [{"path": %s, "target": "t"}]}}`,
		} {
			quoted, err := json.Marshal(tc.path)
			c.Assert(err, IsNil)
			hintsFile := s.writeHints(c, fmt.Sprintf(hints, quoted))

			_, _, err = preseed.CreateSysfsOverlayFromHints(hintsFile)
			c.Check(err, ErrorMatches, `cannot use preseed hints file ".*": `+regexp.QuoteMeta(tc.err), comment)
		}
	}
}

func (s *PreseedHintsSuite) TestPreseedHintsDuplicates(c *C) {
	for _, tc := range []struct {
		hints string
		err   string
	}{
		{`"directories": ["sys/class/leds", "sys/class/leds"]`, `duplicated directory "sys/class/leds"`},
		{`"files": ["sys/class/gpio/export", "sys/class/gpio/export"]`, `duplicated file "sys/class/gpio/export"`},
		{`"symlinks": [{"path": "sys/class/leds/x", "target": "a"}, {"path": "sys/class/leds/x", "target": "b"}]`, `duplicated symlink "sys/class/leds/x"`},
	} {
		hintsFile := s.writeHints(c, `{"format": 1, "sysfs": {`+tc.hints+`}}`)

		_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
		c.Check(err, ErrorMatches, `cannot use preseed hints file ".*": `+regexp.QuoteMeta(tc.err))
	}
}

func (s *PreseedHintsSuite) TestPreseedHintsConflictingEntryTypes(c *C) {
	for _, tc := range []struct {
		hints string
		err   string
	}{
		{`"directories": ["sys/class/leds"], "files": ["sys/class/leds"]`,
			`conflicting entries for "sys/class/leds": directory and file`},
		{`"directories": ["sys/class/leds"], "symlinks": [{"path": "sys/class/leds", "target": "t"}]`,
			`conflicting entries for "sys/class/leds": directory and symlink`},
		{`"files": ["sys/class/leds/x"], "symlinks": [{"path": "sys/class/leds/x", "target": "t"}]`,
			`conflicting entries for "sys/class/leds/x": file and symlink`},
	} {
		hintsFile := s.writeHints(c, `{"format": 1, "sysfs": {`+tc.hints+`}}`)

		_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
		c.Check(err, ErrorMatches, `cannot use preseed hints file ".*": `+regexp.QuoteMeta(tc.err))
	}
}

func (s *PreseedHintsSuite) TestPreseedHintsEntryNestedUnderSymlink(c *C) {
	// creating sys/class/leds/x/brightness would write through the symlink
	// and escape the overlay
	hintsFile := s.writeHints(c, `{
		"format": 1,
		"sysfs": {
			"files": ["sys/class/leds/x/brightness"],
			"symlinks": [{"path": "sys/class/leds/x", "target": "../../devices/platform/leds"}]
		}
	}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot use preseed hints file ".*": entry "sys/class/leds/x/brightness" is nested under symlink "sys/class/leds/x"`)
}

func (s *PreseedHintsSuite) TestPreseedHintsEntryNestedUnderFile(c *C) {
	hintsFile := s.writeHints(c, `{
		"format": 1,
		"sysfs": {
			"directories": ["sys/class/gpio/export/nope"],
			"files": ["sys/class/gpio/export"]
		}
	}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot use preseed hints file ".*": entry "sys/class/gpio/export/nope" is nested under file "sys/class/gpio/export"`)
}

func (s *PreseedHintsSuite) TestPreseedHintsEmptySymlinkTarget(c *C) {
	hintsFile := s.writeHints(c, `{
		"format": 1,
		"sysfs": {"symlinks": [{"path": "sys/class/leds/x", "target": ""}]}
	}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot use preseed hints file ".*": empty target for symlink "sys/class/leds/x"`)
}

func (s *PreseedHintsSuite) TestPreseedHintsTempDirError(c *C) {
	s.AddCleanup(preseed.MockMakeOverlayTempDir(func() (string, error) {
		return "", errors.New("no space left on device")
	}))
	hintsFile := s.writeHints(c, `{"format": 1}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot create sysfs overlay directory: no space left on device`)
}

func (s *PreseedHintsSuite) TestPreseedHintsRemovesPartialOverlayOnError(c *C) {
	overlayDir := s.mockOverlayDir(c)
	// "sys/class/gpio" already exists as a regular file, so creating the
	// directory for the second entry fails after the first one succeeded
	c.Assert(os.MkdirAll(filepath.Join(overlayDir, "sys/class"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(overlayDir, "sys/class/gpio"), nil, 0644), IsNil)

	hintsFile := s.writeHints(c, `{
		"format": 1,
		"sysfs": {"directories": ["sys/class/leds", "sys/class/gpio/gpiochip0"]}
	}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot create sysfs overlay directory "sys/class/gpio/gpiochip0": .*not a directory`)
	// nothing is left behind, including the entry created before the failure
	c.Check(overlayDir, testutil.FileAbsent)
}

func (s *PreseedHintsSuite) TestPreseedHintsFileCreationError(c *C) {
	overlayDir := s.mockOverlayDir(c)
	c.Assert(os.MkdirAll(filepath.Join(overlayDir, "sys/class/gpio/export"), 0755), IsNil)

	hintsFile := s.writeHints(c, `{"format": 1, "sysfs": {"files": ["sys/class/gpio/export"]}}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot create sysfs overlay file "sys/class/gpio/export": .*`)
	c.Check(overlayDir, testutil.FileAbsent)
}

func (s *PreseedHintsSuite) TestPreseedHintsSymlinkCreationError(c *C) {
	overlayDir := s.mockOverlayDir(c)
	c.Assert(os.MkdirAll(filepath.Join(overlayDir, "sys/class/leds"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(overlayDir, "sys/class/leds/x"), nil, 0644), IsNil)

	hintsFile := s.writeHints(c, `{
		"format": 1,
		"sysfs": {"symlinks": [{"path": "sys/class/leds/x", "target": "../../devices/leds"}]}
	}`)

	_, _, err := preseed.CreateSysfsOverlayFromHints(hintsFile)
	c.Check(err, ErrorMatches, `cannot create sysfs overlay symlink "sys/class/leds/x": .*file exists`)
	c.Check(overlayDir, testutil.FileAbsent)
}
