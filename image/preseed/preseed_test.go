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

package preseed_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/image/preseed"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&preseedSuite{})

type preseedSuite struct {
	testutil.BaseTest
}

func (s *preseedSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	restore := squashfs.MockNeedsFuse(false)
	s.BaseTest.AddCleanup(restore)
}

func (s *preseedSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
}

type FakeSeed struct {
	AssertsModel      *asserts.Model
	Essential         []*seed.Snap
	SnapsForMode      map[string][]*seed.Snap
	LoadMetaErr       error
	LoadAssertionsErr error
	UsesSnapd         bool
	loadAssertions    func(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error
}

func mockChrootDirs(c *C, rootDir string, apparmorDir bool) func() {
	if apparmorDir {
		c.Assert(os.MkdirAll(filepath.Join(rootDir, "/sys/kernel/security/apparmor"), 0755), IsNil)
	}
	mockMountInfo := `912 920 0:57 / ${rootDir}/proc rw,nosuid,nodev,noexec,relatime - proc proc rw
914 913 0:7 / ${rootDir}/sys/kernel/security rw,nosuid,nodev,noexec,relatime master:8 - securityfs securityfs rw
915 920 0:58 / ${rootDir}/dev rw,relatime - tmpfs none rw,size=492k,mode=755,uid=100000,gid=100000
`
	return osutil.MockMountInfo(strings.Replace(mockMountInfo, "${rootDir}", rootDir, -1))
}

// Fake implementation of seed.Seed interface

func mockClassicModel() *asserts.Model {
	headers := map[string]interface{}{
		"type":         "model",
		"authority-id": "brand",
		"series":       "16",
		"brand-id":     "brand",
		"model":        "classicbaz-3000",
		"classic":      "true",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}
	return assertstest.FakeAssertion(headers, nil).(*asserts.Model)
}

func (fs *FakeSeed) LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
	if fs.loadAssertions != nil {
		return fs.loadAssertions(db, commitTo)
	}
	return fs.LoadAssertionsErr
}

func (fs *FakeSeed) Model() *asserts.Model {
	return fs.AssertsModel
}

func (fs *FakeSeed) Brand() (*asserts.Account, error) {
	headers := map[string]interface{}{
		"type":         "account",
		"account-id":   "brand",
		"display-name": "fake brand",
		"username":     "brand",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}
	return assertstest.FakeAssertion(headers, nil).(*asserts.Account), nil
}

func (fs *FakeSeed) SetParallelism(int) {}

func (fs *FakeSeed) LoadEssentialMeta(essentialTypes []snap.Type, tm timings.Measurer) error {
	return fs.LoadMetaErr
}

func (fs *FakeSeed) LoadEssentialMetaWithSnapHandler(essentialTypes []snap.Type, handler seed.ContainerHandler, tm timings.Measurer) error {
	return fs.LoadMetaErr
}

func (fs *FakeSeed) LoadMeta(mode string, handler seed.ContainerHandler, tm timings.Measurer) error {
	return fs.LoadMetaErr
}

func (fs *FakeSeed) UsesSnapdSnap() bool {
	return fs.UsesSnapd
}

func (fs *FakeSeed) EssentialSnaps() []*seed.Snap {
	return fs.Essential
}

func (fs *FakeSeed) ModeSnaps(mode string) ([]*seed.Snap, error) {
	return fs.SnapsForMode[mode], nil
}

func (fs *FakeSeed) NumSnaps() int {
	return 0
}

func (fs *FakeSeed) Iter(f func(sn *seed.Snap) error) error {
	return nil
}

func (s *preseedSuite) TestSystemSnapFromSeed(c *C) {
	tmpDir := c.MkDir()

	restore := preseed.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) {
		return &FakeSeed{
			AssertsModel: mockClassicModel(),
			Essential:    []*seed.Snap{{Path: "/some/path/core", SideInfo: &snap.SideInfo{RealName: "core"}}},
		}, nil
	})
	defer restore()

	path, _, err := preseed.SystemSnapFromSeed(tmpDir, "")
	c.Assert(err, IsNil)
	c.Check(path, Equals, "/some/path/core")
}

func (s *preseedSuite) TestSystemSnapFromSnapdSeed(c *C) {
	tmpDir := c.MkDir()

	restore := preseed.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) {
		return &FakeSeed{
			AssertsModel: mockClassicModel(),
			Essential:    []*seed.Snap{{Path: "/some/path/snapd.snap", SideInfo: &snap.SideInfo{RealName: "snapd"}}},
			UsesSnapd:    true,
		}, nil
	})
	defer restore()

	path, _, err := preseed.SystemSnapFromSeed(tmpDir, "")
	c.Assert(err, IsNil)
	c.Check(path, Equals, "/some/path/snapd.snap")
}

func (s *preseedSuite) TestSystemSnapFromSeedOpenError(c *C) {
	tmpDir := c.MkDir()

	restore := preseed.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) { return nil, fmt.Errorf("fail") })
	defer restore()

	_, _, err := preseed.SystemSnapFromSeed(tmpDir, "")
	c.Assert(err, ErrorMatches, "fail")
}

func (s *preseedSuite) TestSystemSnapFromSeedErrors(c *C) {
	tmpDir := c.MkDir()

	fakeSeed := &FakeSeed{}
	fakeSeed.AssertsModel = mockClassicModel()

	restore := preseed.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) { return fakeSeed, nil })
	defer restore()

	fakeSeed.Essential = []*seed.Snap{{Path: "", SideInfo: &snap.SideInfo{RealName: "core"}}}
	_, _, err := preseed.SystemSnapFromSeed(tmpDir, "")
	c.Assert(err, ErrorMatches, "core snap not found")

	fakeSeed.Essential = []*seed.Snap{{Path: "/some/path", SideInfo: &snap.SideInfo{RealName: "foosnap"}}}
	_, _, err = preseed.SystemSnapFromSeed(tmpDir, "")
	c.Assert(err, ErrorMatches, "core snap not found")

	fakeSeed.LoadMetaErr = fmt.Errorf("load meta failed")
	_, _, err = preseed.SystemSnapFromSeed(tmpDir, "")
	c.Assert(err, ErrorMatches, "load meta failed")

	fakeSeed.LoadMetaErr = nil
	fakeSeed.LoadAssertionsErr = fmt.Errorf("load assertions failed")
	_, _, err = preseed.SystemSnapFromSeed(tmpDir, "")
	c.Assert(err, ErrorMatches, "load assertions failed")
}

func (s *preseedSuite) TestChooseTargetSnapdVersion(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "usr/lib/snapd/"), 0755), IsNil)

	targetSnapdRoot := filepath.Join(tmpDir, "target-core-mounted-here")
	c.Assert(os.MkdirAll(filepath.Join(targetSnapdRoot, "usr/lib/snapd/"), 0755), IsNil)
	restoreMountPath := preseed.MockSnapdMountPath(targetSnapdRoot)
	defer restoreMountPath()

	var versions = []struct {
		fromSnap        string
		fromDeb         string
		expectedPath    string
		expectedVersion string
		expectedErr     string
	}{
		{
			fromDeb:  "2.44.0",
			fromSnap: "2.45.3+git123",
			// snap version wins
			expectedVersion: "2.45.3+git123",
			expectedPath:    filepath.Join(tmpDir, "target-core-mounted-here/usr/lib/snapd/snapd"),
		},
		{
			fromDeb:  "2.44.0",
			fromSnap: "2.44.0",
			// snap version wins
			expectedVersion: "2.44.0",
			expectedPath:    filepath.Join(tmpDir, "target-core-mounted-here/usr/lib/snapd/snapd"),
		},
		{
			fromDeb:  "2.45.1+20.04",
			fromSnap: "2.45.1",
			// deb version wins
			expectedVersion: "2.45.1+20.04",
			expectedPath:    filepath.Join(tmpDir, "usr/lib/snapd/snapd"),
		},
		{
			fromDeb:  "2.45.2",
			fromSnap: "2.45.1",
			// deb version wins
			expectedVersion: "2.45.2",
			expectedPath:    filepath.Join(tmpDir, "usr/lib/snapd/snapd"),
		},
		{
			fromSnap:    "2.45.1",
			expectedErr: fmt.Sprintf("cannot open snapd info file %q.*", filepath.Join(tmpDir, "usr/lib/snapd/info")),
		},
		{
			fromDeb:     "2.45.1",
			expectedErr: fmt.Sprintf("cannot open snapd info file %q.*", filepath.Join(tmpDir, "target-core-mounted-here/usr/lib/snapd/info")),
		},
	}

	for _, test := range versions {
		infoFile := filepath.Join(tmpDir, "usr/lib/snapd/info")
		os.Remove(infoFile)
		if test.fromDeb != "" {
			c.Assert(os.WriteFile(infoFile, []byte(fmt.Sprintf("VERSION=%s", test.fromDeb)), 0644), IsNil)
		}
		infoFile = filepath.Join(targetSnapdRoot, "usr/lib/snapd/info")
		os.Remove(infoFile)
		if test.fromSnap != "" {
			c.Assert(os.WriteFile(infoFile, []byte(fmt.Sprintf("VERSION=%s", test.fromSnap)), 0644), IsNil)
		}

		targetSnapd, err := preseed.ChooseTargetSnapdVersion()
		if test.expectedErr != "" {
			c.Assert(err, ErrorMatches, test.expectedErr)
		} else {
			c.Assert(err, IsNil)
			c.Assert(targetSnapd, NotNil)
			path, version := preseed.SnapdPathAndVersion(targetSnapd)
			c.Check(path, Equals, test.expectedPath)
			c.Check(version, Equals, test.expectedVersion)
		}
	}
}

func (s *preseedSuite) TestCreatePreseedArtifact(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)

	prepareDir := filepath.Join(tmpDir, "prepare-dir")
	c.Assert(os.MkdirAll(filepath.Join(prepareDir, "system-seed/systems/20220203"), 0755), IsNil)

	writableDir := filepath.Join(tmpDir, "writable")
	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)

	mockTar := testutil.MockCommand(c, "tar", "")
	defer mockTar.Restore()

	c.Assert(os.MkdirAll(filepath.Join(writableDir, "system-data/etc/bar"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(writableDir, "system-data/baz"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(writableDir, "system-data/etc/bar/a"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(writableDir, "system-data/etc/bar/x"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(writableDir, "system-data/baz/b"), nil, 0644), IsNil)

	const exportFileContents = `{
"exclude": ["/etc/bar/x*"],
"include": ["/etc/bar/a", "/baz/*"]
}`
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "/usr/lib/snapd"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(tmpDir, "/usr/lib/snapd/preseed.json"), []byte(exportFileContents), 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(prepareDir, "system-seed/systems/20220203/preseed.tgz"), nil, 0644), IsNil)

	opts := &preseed.CoreOptions{
		PrepareImageDir: prepareDir,
	}
	popts := &preseed.PreseedCoreOptions{
		CoreOptions:      *opts,
		PreseedChrootDir: tmpDir,
		SystemLabel:      "20220203",
		WritableDir:      writableDir,
	}

	_, err := preseed.CreatePreseedArtifact(popts)
	c.Assert(err, IsNil)
	c.Check(mockTar.Calls(), DeepEquals, [][]string{
		{"tar", "-czf", filepath.Join(tmpDir, "prepare-dir/system-seed/systems/20220203/preseed.tgz"), "-p", "-C",
			filepath.Join(writableDir, "system-data"), "--exclude", "/etc/bar/x*", "etc/bar/a", "baz/b"},
	})
}
