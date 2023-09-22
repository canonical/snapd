// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package snap_test

import (
	"errors"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
)

type FakeContainer struct {
	*snapdir.SnapDir

	readFileInput  string
	readFileOutput []byte
	readFileError  error
}

func (s *FakeContainer) ReadFile(file string) (content []byte, err error) {
	s.readFileInput = file
	return s.readFileOutput, s.readFileError
}

type snapshotSuite struct{}

var _ = Suite(&snapshotSuite{})

var snapshotHappyYaml = []byte(`exclude:
  - $SNAP_DATA/one
  - $SNAP_COMMON/two
  - $SNAP_USER_DATA/three*
  - $SNAP_USER_COMMON/fo*ur`)

var snapshotHappyExpectedExclude = []string{
	"$SNAP_DATA/one",
	"$SNAP_COMMON/two",
	"$SNAP_USER_DATA/three*",
	"$SNAP_USER_COMMON/fo*ur",
}

func (s *snapshotSuite) TestValidateErrors(c *C) {
	const mustStartWithError = "snapshot exclude path must start with one of.*"
	const pathInvalidCharsError = "snapshot exclude path contains invalid characters.*"
	const pathNotCleanError = "snapshot exclude path not clean.*"

	testMap := map[string]struct {
		snapshotOptions snap.SnapshotOptions
		expectedError   string
	}{
		"must-start-with-1": {snap.SnapshotOptions{Exclude: []string{"/home/ubuntu"}}, mustStartWithError},
		"must-start-with-2": {snap.SnapshotOptions{Exclude: []string{"$SNAP_COMMON_STUFF"}}, mustStartWithError},
		"path-not-clean-1":  {snap.SnapshotOptions{Exclude: []string{"$SNAP_DATA/../../meh"}}, pathNotCleanError},
		"path-not-clean-2":  {snap.SnapshotOptions{Exclude: []string{"$SNAP_DATA/"}}, pathNotCleanError},
		"invalid-chars-1":   {snap.SnapshotOptions{Exclude: []string{"$SNAP_DATA/{one,two}"}}, pathInvalidCharsError},
		"invalid-chars-2":   {snap.SnapshotOptions{Exclude: []string{"$SNAP_DATA/tree**"}}, pathInvalidCharsError},
		"invalid-chars-3":   {snap.SnapshotOptions{Exclude: []string{"$SNAP_DATA/foo[12]"}}, pathInvalidCharsError},
		"invalid-chars-4":   {snap.SnapshotOptions{Exclude: []string{"$SNAP_DATA/bar?"}}, pathInvalidCharsError},
	}

	for name, test := range testMap {
		snapshotOptionsCopy := test.snapshotOptions
		c.Check(test.snapshotOptions.Validate(), ErrorMatches, test.expectedError, Commentf("test: %q", name))
		c.Check(test.snapshotOptions, DeepEquals, snapshotOptionsCopy)
	}
}

func (s *snapshotSuite) TestValidateHappy(c *C) {
	testMap := map[string]struct {
		snapshotOptions snap.SnapshotOptions
	}{
		"exclude-empty":   {snap.SnapshotOptions{Exclude: []string{}}},
		"exclude-typical": {snap.SnapshotOptions{Exclude: snapshotHappyExpectedExclude}},
	}

	for name, test := range testMap {
		c.Check(test.snapshotOptions.Validate(), IsNil, Commentf("test: %q", name))
	}
}

func (s *snapshotSuite) TestMergeDynamicExcludesError(c *C) {
	snapshotOptions := snap.SnapshotOptions{Exclude: snapshotHappyExpectedExclude}
	dynamicExcludes := []string{"/home/ubuntu"}
	snapshotOptionsCopy := snapshotOptions
	c.Check(snapshotOptions.MergeDynamicExcludes(dynamicExcludes), ErrorMatches, "snapshot exclude path must start with one of.*")
	c.Check(snapshotOptions, DeepEquals, snapshotOptionsCopy)
}

func (s *snapshotSuite) TestMergeDynamicExcludesHappy(c *C) {
	snapshotOptions := snap.SnapshotOptions{Exclude: snapshotHappyExpectedExclude}
	snapshotOptionsMerged := snap.SnapshotOptions{
		Exclude: append(snapshotHappyExpectedExclude, snapshotHappyExpectedExclude...),
	}

	testMap := map[string]struct {
		dynamicExcludes []string
		expectedOptions snap.SnapshotOptions
	}{
		"exclude-nil":     {nil, snapshotOptions},
		"exclude-empty":   {[]string{}, snapshotOptions},
		"exclude-typical": {snapshotHappyExpectedExclude, snapshotOptionsMerged},
	}

	for name, test := range testMap {
		snapshotOptionsCopy := snapshotOptions
		c.Check(snapshotOptionsCopy.MergeDynamicExcludes(test.dynamicExcludes), IsNil, Commentf("test: %q", name))
		c.Check(snapshotOptionsCopy, DeepEquals, test.expectedOptions, Commentf("test: %q", name))
	}
}

func (s *snapshotSuite) TestUnset(c *C) {
	testMap := map[string]struct {
		options *snap.SnapshotOptions
		isUnset bool
	}{
		"exclude-empty":   {options: &snap.SnapshotOptions{[]string{}}, isUnset: true},
		"exclude-nil":     {options: &snap.SnapshotOptions{}, isUnset: true},
		"exclude-typical": {options: &snap.SnapshotOptions{snapshotHappyExpectedExclude}, isUnset: false},
	}

	for name, test := range testMap {
		c.Check(test.options.Unset(), Equals, test.isUnset, Commentf("test: %q", name))
	}
}

func (s *snapshotSuite) TestReadSnapshotYamlOpenFails(c *C) {
	var returnedError error
	defer snap.MockOsOpen(func(string) (*os.File, error) {
		return nil, returnedError
	})()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}}

	// Try a generic error, this is reported as such
	returnedError = errors.New("Some error")
	_, err := snap.ReadSnapshotYaml(info)
	c.Check(err, ErrorMatches, "Some error")

	// But if the file is not found, that's just a nil error
	returnedError = os.ErrNotExist
	_, err = snap.ReadSnapshotYaml(info)
	c.Check(err, IsNil)
}

func (s *snapshotSuite) TestReadSnapshotYamlFromSnapFileFails(c *C) {
	container := &FakeContainer{
		readFileError: errors.New("cannot do stuff"),
	}
	opts, err := snap.ReadSnapshotYamlFromSnapFile(container)
	c.Check(container.readFileInput, Equals, "meta/snapshots.yaml")
	c.Check(opts, IsNil)
	c.Check(err, ErrorMatches, "cannot do stuff")
}

func (s *snapshotSuite) TestReadSnapshotYamlFromSnapFileHappy(c *C) {
	container := &FakeContainer{
		readFileOutput: snapshotHappyYaml,
	}
	opts, err := snap.ReadSnapshotYamlFromSnapFile(container)
	c.Check(container.readFileInput, Equals, "meta/snapshots.yaml")
	c.Check(err, IsNil)
	c.Check(opts, DeepEquals, &snap.SnapshotOptions{
		Exclude: snapshotHappyExpectedExclude,
	})
}

func (s *snapshotSuite) TestReadSnapshotYamlFailures(c *C) {
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}}

	for _, testData := range []struct {
		contents      string
		expectedError string
	}{
		{
			"", "cannot read snapshot manifest: EOF",
		},
		{
			"invalid", "cannot read snapshot manifest: yaml: unmarshal errors:\n.*",
		},
		{
			"exclude:\n  - /home/ubuntu", "snapshot exclude path must start with one of.*",
		},
	} {
		manifestFile := filepath.Join(c.MkDir(), "snapshots.yaml")
		err := os.WriteFile(manifestFile, []byte(testData.contents), 0644)
		c.Assert(err, IsNil)
		defer snap.MockOsOpen(func(string) (*os.File, error) {
			return os.Open(manifestFile)
		})()

		_, err = snap.ReadSnapshotYaml(info)
		c.Check(err, ErrorMatches, testData.expectedError, Commentf("%s", testData.contents))
	}
}

func (s *snapshotSuite) TestReadSnapshotYamlHappy(c *C) {
	manifestFile := filepath.Join(c.MkDir(), "snapshots.yaml")
	err := os.WriteFile(manifestFile, []byte(snapshotHappyYaml), 0644)
	c.Assert(err, IsNil)

	defer snap.MockOsOpen(func(path string) (*os.File, error) {
		return os.Open(manifestFile)
	})()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}}

	opts, err := snap.ReadSnapshotYaml(info)
	c.Check(err, IsNil)
	c.Check(opts, DeepEquals, &snap.SnapshotOptions{
		Exclude: snapshotHappyExpectedExclude,
	})
}
