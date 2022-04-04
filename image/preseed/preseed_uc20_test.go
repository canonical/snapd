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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/image/preseed"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type fakeKeyMgr struct {
	key asserts.PrivateKey
}

func (f *fakeKeyMgr) Put(privKey asserts.PrivateKey) error                  { return nil }
func (f *fakeKeyMgr) Get(keyID string) (asserts.PrivateKey, error)          { return f.key, nil }
func (f *fakeKeyMgr) Delete(keyID string) error                             { return nil }
func (f *fakeKeyMgr) GetByName(keyNname string) (asserts.PrivateKey, error) { return f.key, nil }
func (f *fakeKeyMgr) Export(keyName string) ([]byte, error)                 { return nil, nil }
func (f *fakeKeyMgr) List() ([]asserts.ExternalKeyInfo, error)              { return nil, nil }
func (f *fakeKeyMgr) DeleteByName(keyName string) error                     { return nil }

func mockUC20Model() *asserts.Model {
	headers := map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model-uc20",
		"display-name": "My Model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"timestamp":    "2019-11-01T08:00:00+00:00",
		"snaps": []interface{}{
			map[string]interface{}{
				"name": "pc-kernel",
				"id":   "pckernelidididididididididididid",
				"type": "kernel",
			},
			map[string]interface{}{
				"name": "pc",
				"id":   "pcididididididididididididididid",
				"type": "gadget",
			},
		},
	}
	return assertstest.FakeAssertion(headers, nil).(*asserts.Model)
}

func (s *preseedSuite) TestRunPreseedUC20Happy(c *C) {
	restoreSeedOpen := preseed.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) {
		return &FakeSeed{
			AssertsModel: mockUC20Model(),
			UsesSnapd:    true,
			Essential: []*seed.Snap{{
				Path: "/some/path/snapd.snap",
				SideInfo: &snap.SideInfo{
					RealName: "snapd",
					SnapID:   "snapdidididididididididididididd",
					Revision: snap.R("1")}},
			},
			SnapsForMode: map[string][]*seed.Snap{
				"run": {{
					Path: "/some/path/foo.snap",
					SideInfo: &snap.SideInfo{
						RealName: "foo"},
				}}},
		}, nil
	})
	defer restoreSeedOpen()

	restoreSaveAssertion := preseed.MockSaveAssertion(func(*asserts.Database, asserts.Assertion, *asserts.Model) error {
		return nil
	})
	defer restoreSaveAssertion()

	testKey, _ := assertstest.GenerateKey(752)
	keyMgr := &fakeKeyMgr{testKey}
	restoreGetKeypairMgr := preseed.MockGetKeypairManager(func() (signtool.KeypairManager, error) {
		return keyMgr, nil
	})
	defer restoreGetKeypairMgr()

	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)
	defer mockChrootDirs(c, tmpDir, true)()

	mockChootCmd := testutil.MockCommand(c, "chroot", "")
	defer mockChootCmd.Restore()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockUmountCmd := testutil.MockCommand(c, "umount", "")
	defer mockUmountCmd.Restore()

	preseedTmpDir := filepath.Join(tmpDir, "preseed-tmp")
	restoreMakePreseedTmpDir := preseed.MockMakePreseedTempDir(func() (string, error) {
		return preseedTmpDir, nil
	})
	defer restoreMakePreseedTmpDir()

	writableTmpDir := filepath.Join(tmpDir, "writable-tmp")
	restoreMakeWritableTempDir := preseed.MockMakeWritableTempDir(func() (string, error) {
		return writableTmpDir, nil
	})
	defer restoreMakeWritableTempDir()

	c.Assert(os.MkdirAll(filepath.Join(writableTmpDir, "system-data/etc/bar"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(writableTmpDir, "system-data/etc/bar/a"), nil, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(writableTmpDir, "system-data/etc/bar/b"), nil, 0644), IsNil)

	mockTar := testutil.MockCommand(c, "tar", "")
	defer mockTar.Restore()

	const exportFileContents = `{
"exclude": ["foo"],
"include": ["/etc/bar/a", "/etc/bar/b"]
}`

	c.Assert(os.MkdirAll(filepath.Join(preseedTmpDir, "usr/lib/snapd"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(preseedTmpDir, "usr/lib/snapd/preseed.json"), []byte(exportFileContents), 0644), IsNil)

	mockWritablePaths := testutil.MockCommand(c, filepath.Join(preseedTmpDir, "/usr/lib/core/handle-writable-paths"), "")
	defer mockWritablePaths.Restore()

	restore := osutil.MockMountInfo(fmt.Sprintf(`130 30 42:1 / %s/somepath rw,relatime shared:54 - ext4 /some/path rw
`, preseedTmpDir))
	defer restore()

	targetSnapdRoot := filepath.Join(tmpDir, "target-core-mounted-here")
	restoreMountPath := preseed.MockSnapdMountPath(targetSnapdRoot)
	defer restoreMountPath()

	restoreSystemSnapFromSeed := preseed.MockSystemSnapFromSeed(func(string, string) (string, string, error) { return "/a/snapd.snap", "/a/base.snap", nil })
	defer restoreSystemSnapFromSeed()

	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "system-seed/systems/20220203"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(tmpDir, "system-seed/systems/20220203/preseed.tgz"), []byte(`hello world`), 0644), IsNil)

	c.Assert(preseed.Core20(tmpDir, ""), IsNil)

	c.Check(mockChootCmd.Calls()[0], DeepEquals, []string{"chroot", preseedTmpDir, "/usr/lib/snapd/snapd"})

	c.Check(mockMountCmd.Calls(), DeepEquals, [][]string{
		{"mount", "-o", "loop", "/a/base.snap", preseedTmpDir},
		{"mount", "-o", "loop", "/a/snapd.snap", targetSnapdRoot},
		{"mount", "-t", "tmpfs", "tmpfs", filepath.Join(preseedTmpDir, "run")},
		{"mount", "-t", "tmpfs", "tmpfs", filepath.Join(preseedTmpDir, "var/tmp")},
		{"mount", "--bind", filepath.Join(preseedTmpDir, "/var/tmp"), filepath.Join(preseedTmpDir, "tmp")},
		{"mount", "-t", "proc", "proc", filepath.Join(preseedTmpDir, "proc")},
		{"mount", "-t", "sysfs", "sysfs", filepath.Join(preseedTmpDir, "sys")},
		{"mount", "-t", "devtmpfs", "udev", filepath.Join(preseedTmpDir, "dev")},
		{"mount", "-t", "securityfs", "securityfs", filepath.Join(preseedTmpDir, "sys/kernel/security")},
		{"mount", "--bind", writableTmpDir, filepath.Join(preseedTmpDir, "writable")},
		{"mount", "--bind", filepath.Join(writableTmpDir, "system-data/var/lib/snapd"), filepath.Join(preseedTmpDir, "var/lib/snapd")},
		{"mount", "--bind", filepath.Join(writableTmpDir, "system-data/var/cache/snapd"), filepath.Join(preseedTmpDir, "var/cache/snapd")},
		{"mount", "--bind", filepath.Join(writableTmpDir, "system-data/var/cache/apparmor"), filepath.Join(preseedTmpDir, "var/cache/apparmor")},
		{"mount", "--bind", filepath.Join(writableTmpDir, "system-data/var/snap"), filepath.Join(preseedTmpDir, "var/snap")},
		{"mount", "--bind", filepath.Join(writableTmpDir, "system-data/snap"), filepath.Join(preseedTmpDir, "snap")},
		{"mount", "--bind", filepath.Join(writableTmpDir, "system-data/etc/systemd"), filepath.Join(preseedTmpDir, "etc/systemd")},
		{"mount", "--bind", filepath.Join(writableTmpDir, "system-data/etc/dbus-1"), filepath.Join(preseedTmpDir, "etc/dbus-1")},
		{"mount", "--bind", filepath.Join(writableTmpDir, "system-data/etc/udev/rules.d"), filepath.Join(preseedTmpDir, "etc/udev/rules.d")},
		{"mount", "--bind", filepath.Join(targetSnapdRoot, "/usr/lib/snapd"), filepath.Join(preseedTmpDir, "usr/lib/snapd")},
		{"mount", "--bind", filepath.Join(tmpDir, "system-seed"), filepath.Join(preseedTmpDir, "var/lib/snapd/seed")},
	})

	c.Check(mockTar.Calls(), DeepEquals, [][]string{
		{"tar", "-czf", filepath.Join(tmpDir, "system-seed/systems/20220203/preseed.tgz"), "-p", "-C",
			filepath.Join(writableTmpDir, "system-data"), "--exclude", "foo", "etc/bar/a", "etc/bar/b"},
	})

	c.Check(mockUmountCmd.Calls(), DeepEquals, [][]string{
		{"umount", filepath.Join(preseedTmpDir, "var/lib/snapd/seed")},
		{"umount", filepath.Join(preseedTmpDir, "usr/lib/snapd")},
		{"umount", filepath.Join(preseedTmpDir, "etc/udev/rules.d")},
		{"umount", filepath.Join(preseedTmpDir, "etc/dbus-1")},
		{"umount", filepath.Join(preseedTmpDir, "etc/systemd")},
		{"umount", filepath.Join(preseedTmpDir, "snap")},
		{"umount", filepath.Join(preseedTmpDir, "var/snap")},
		{"umount", filepath.Join(preseedTmpDir, "var/cache/apparmor")},
		{"umount", filepath.Join(preseedTmpDir, "var/cache/snapd")},
		{"umount", filepath.Join(preseedTmpDir, "var/lib/snapd")},
		{"umount", filepath.Join(preseedTmpDir, "writable")},
		{"umount", filepath.Join(preseedTmpDir, "sys/kernel/security")},
		{"umount", filepath.Join(preseedTmpDir, "dev")},
		{"umount", filepath.Join(preseedTmpDir, "sys")},
		{"umount", filepath.Join(preseedTmpDir, "proc")},
		{"umount", filepath.Join(preseedTmpDir, "tmp")},
		{"umount", filepath.Join(preseedTmpDir, "var/tmp")},
		{"umount", filepath.Join(preseedTmpDir, "run")},
		{"umount", filepath.Join(tmpDir, "target-core-mounted-here")},
		{"umount", preseedTmpDir},
		// from handle-writable-paths
		{"umount", filepath.Join(preseedTmpDir, "somepath")},
	})

	// validity check; -1 to account for handle-writable-paths mock which doesn’t trigger mount in the test
	c.Check(len(mockMountCmd.Calls()), Equals, len(mockUmountCmd.Calls())-1)

	preseedAssertionPath := filepath.Join(tmpDir, "system-seed/systems/20220203/assertions/preseed")
	c.Assert(osutil.FileExists(preseedAssertionPath), Equals, true)
	data, err := ioutil.ReadFile(preseedAssertionPath)
	c.Assert(err, IsNil)
	as, err := asserts.Decode(data)
	c.Assert(err, IsNil)
	c.Check(as.Type(), Equals, asserts.PreseedType)

	preseedAs := as.(*asserts.Preseed)
	c.Check(preseedAs.Revision(), Equals, 1)
	c.Check(preseedAs.Series(), Equals, "16")
	c.Check(preseedAs.AuthorityID(), Equals, "my-brand")
	c.Check(preseedAs.BrandID(), Equals, "my-brand")
	c.Check(preseedAs.Model(), Equals, "my-model-uc20")
	c.Check(preseedAs.SystemLabel(), Equals, "20220203")
	c.Check(preseedAs.ArtifactSHA3_384(), Equals, "g7_yjd4bG_WBAHHGZDwI5bBb24Nu_9cLQD6o6gpjTcSZfrEFOqNZP1kPnGNjDdkL")
	c.Check(preseedAs.Snaps(), DeepEquals, []*asserts.PreseedSnap{{
		Name:     "snapd",
		SnapID:   "snapdidididididididididididididd",
		Revision: 1,
	}, {
		Name: "foo",
	}})
}
