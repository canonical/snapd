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
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/image/preseed"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/tooling"
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

type toolingStore struct {
	*seedtest.SeedSnaps
}

func (t *toolingStore) SnapAction(_ context.Context, curSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, _ *auth.UserState, _ *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	panic("not expected")
}

func (s *toolingStore) Download(ctx context.Context, name, targetFn string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *store.DownloadOptions) error {
	panic("not expected")
}

func (s *toolingStore) Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error) {
	ref := &asserts.Ref{Type: assertType, PrimaryKey: primaryKey}
	as, err := ref.Resolve(s.StoreSigning.Find)
	if err != nil {
		return nil, err
	}
	return as, nil
}

func (s *toolingStore) SeqFormingAssertion(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
	panic("not expected")
}

func (s *toolingStore) SetAssertionMaxFormats(maxFormats map[string]int) {
	panic("not implemented")
}

// list of test overlays
var sysFsOverlaysGood = []string{"class/backlight", "class/bluetooth", "class/gpio", "class/leds", "class/ptp", "class/pwm", "class/rtc", "class/video4linux", "devices/platform", "devices/pci0000:00"}

var sysFsOverlaysBad = []string{"class/backlight-xxx", "class/spi", "devices/pci"}

func (s *preseedSuite) testRunPreseedUC20Happy(c *C, customAppArmorFeaturesDir, sysfsOverlay string) {

	testKey, _ := assertstest.GenerateKey(752)

	ts := &toolingStore{&seedtest.SeedSnaps{}}
	ts.SeedSnaps.SetupAssertSigning("canonical")
	ts.Brands.Register("my-brand", testKey, map[string]interface{}{
		"verification": "verified",
	})

	assertstest.AddMany(ts.StoreSigning, ts.Brands.AccountsAndKeys("my-brand")...)

	tsto := tooling.MockToolingStore(ts)
	restoreToolingStore := preseed.MockNewToolingStoreFromModel(func(model *asserts.Model, fallbackArchitecture string) (*tooling.ToolingStore, error) {
		return tsto, nil
	})
	defer restoreToolingStore()

	restoreTrusted := preseed.MockTrusted(ts.StoreSigning.Trusted)
	defer restoreTrusted()

	model := ts.Brands.Model("my-brand", "my-model-uc20", map[string]interface{}{
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
	})

	restoreSeedOpen := preseed.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) {
		return &FakeSeed{
			AssertsModel: model,
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
			loadAssertions: func(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
				batch := asserts.NewBatch(nil)
				c.Assert(batch.Add(ts.StoreSigning.StoreAccountKey("")), IsNil)
				c.Assert(commitTo(batch), IsNil)
				return nil
			},
		}, nil
	})
	defer restoreSeedOpen()

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
	c.Assert(os.WriteFile(filepath.Join(writableTmpDir, "system-data/etc/bar/a"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(writableTmpDir, "system-data/etc/bar/b"), nil, 0644), IsNil)

	mockTar := testutil.MockCommand(c, "tar", "")
	defer mockTar.Restore()

	const exportFileContents = `{
"exclude": ["foo"],
"include": ["/etc/bar/a", "/etc/bar/b"]
}`

	c.Assert(os.MkdirAll(filepath.Join(preseedTmpDir, "usr/lib/snapd"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(preseedTmpDir, "usr/lib/snapd/preseed.json"), []byte(exportFileContents), 0644), IsNil)

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
	c.Assert(os.WriteFile(filepath.Join(tmpDir, "system-seed/systems/20220203/preseed.tgz"), []byte(`hello world`), 0644), IsNil)

	opts := &preseed.CoreOptions{
		PrepareImageDir:           tmpDir,
		PreseedSignKey:            "",
		AppArmorKernelFeaturesDir: customAppArmorFeaturesDir,
		SysfsOverlay:              sysfsOverlay,
	}

	c.Assert(preseed.Core20(opts), IsNil)

	c.Check(mockChootCmd.Calls()[0], DeepEquals, []string{"chroot", preseedTmpDir, "/usr/lib/snapd/snapd"})

	expectedMountCalls := [][]string{
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
		{"mount", "--bind", filepath.Join(writableTmpDir, "system-data/var/lib/extrausers"), filepath.Join(preseedTmpDir, "var/lib/extrausers")},
		{"mount", "--bind", filepath.Join(targetSnapdRoot, "/usr/lib/snapd"), filepath.Join(preseedTmpDir, "usr/lib/snapd")},
		{"mount", "--bind", filepath.Join(tmpDir, "system-seed"), filepath.Join(preseedTmpDir, "var/lib/snapd/seed")},
	}

	expectedUmountCalls := [][]string{
		{"umount", filepath.Join(preseedTmpDir, "var/lib/snapd/seed")},
		{"umount", filepath.Join(preseedTmpDir, "usr/lib/snapd")},
		{"umount", filepath.Join(preseedTmpDir, "var/lib/extrausers")},
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
		// from handle-writable-paths
		{"umount", filepath.Join(preseedTmpDir, "somepath")},
		{"umount", preseedTmpDir},
	}

	if sysfsOverlay != "" {
		for i, dir := range sysFsOverlaysGood {
			const sysFsMountFirstIndex = 10
			expectedMountCalls = append(expectedMountCalls[:sysFsMountFirstIndex+i+1], expectedMountCalls[sysFsMountFirstIndex+i:]...)
			expectedMountCalls[sysFsMountFirstIndex+i] = []string{"mount", "--bind", filepath.Join(sysfsOverlay, "sys", dir), filepath.Join(preseedTmpDir, "sys", dir)}
			// order of umounts is reversed, prepend
			const sysFsUmountFirstIndex = 11
			expectedUmountCalls = append(expectedUmountCalls[:sysFsUmountFirstIndex+1], expectedUmountCalls[sysFsUmountFirstIndex:]...)
			expectedUmountCalls[sysFsUmountFirstIndex] = []string{"umount", filepath.Join(preseedTmpDir, "sys", dir)}
		}
	}

	if customAppArmorFeaturesDir != "" {
		expectedMountCalls = append(expectedMountCalls, []string{"mount", "--bind", "/custom-aa-features", filepath.Join(preseedTmpDir, "sys/kernel/security/apparmor/features")})
		// order of umounts is reversed, prepend
		expectedUmountCalls = append([][]string{{"umount", filepath.Join(preseedTmpDir, "/sys/kernel/security/apparmor/features")}}, expectedUmountCalls...)
	}
	c.Check(mockMountCmd.Calls(), DeepEquals, expectedMountCalls)

	c.Check(mockTar.Calls(), DeepEquals, [][]string{
		{"tar", "-czf", filepath.Join(tmpDir, "system-seed/systems/20220203/preseed.tgz"), "-p", "-C",
			filepath.Join(writableTmpDir, "system-data"), "--exclude", "foo", "etc/bar/a", "etc/bar/b"},
	})

	c.Check(mockUmountCmd.Calls(), DeepEquals, expectedUmountCalls)

	// validity check; -1 to account for handle-writable-paths mock which doesn’t trigger mount in the test
	c.Check(len(mockMountCmd.Calls()), Equals, len(mockUmountCmd.Calls())-1)

	preseedAssertionPath := filepath.Join(tmpDir, "system-seed/systems/20220203/preseed")
	r, err := os.Open(preseedAssertionPath)
	c.Assert(err, IsNil)
	defer r.Close()

	// check directory targetSnapdRoot was deleted
	_, err = os.Stat(targetSnapdRoot)
	c.Assert(err, NotNil)
	c.Check(os.IsNotExist(err), Equals, true)

	seen := make(map[string]bool)
	dec := asserts.NewDecoder(r)
	for {
		as, err := dec.Decode()
		if err == io.EOF {
			break
		}
		c.Assert(err, IsNil)

		tpe := as.Type().Name

		switch as.Type() {
		case asserts.AccountKeyType:
			acckeyAs := as.(*asserts.AccountKey)
			tpe = fmt.Sprintf("%s:%s", as.Type().Name, acckeyAs.AccountID())
		case asserts.PreseedType:
			preseedAs := as.(*asserts.Preseed)
			c.Check(preseedAs.Revision(), Equals, 0)
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
		default:
			c.Fatalf("unexpected assertion: %s", as.Type().Name)
		}
		seen[tpe] = true
	}

	c.Check(seen, DeepEquals, map[string]bool{
		"account-key:my-brand": true,
		"preseed":              true,
	})
}

func (s *preseedSuite) TestRunPreseedUC20Happy(c *C) {
	s.testRunPreseedUC20Happy(c, "", "")
}

func (s *preseedSuite) TestRunPreseedUC20HappyCustomApparmorFeaturesDir(c *C) {
	s.testRunPreseedUC20Happy(c, "/custom-aa-features", "")
}

func (s *preseedSuite) TestRunPreseedUC20HappySysfsOverlay(c *C) {
	// create example sysfs overlay structure
	// backlight bluetooth gpio leds  ptp pwm rtc video4linux
	// sys
	//  ├── class
	//  │   ├── backlight
	//  │   │   └── intel_backlight  ->  ../../devices/pci0000:00/0000:00:02.0
	//  │   ├── bluetooth
	//  │   │   └── hci0             ->  ../../devices/platform/e4000000.s_ahb
	//  │   ├── gpio
	//  │   │   └── gpio1            -> ../../devices/platform/e4000000.s_ahb
	//  │   ├── leds
	//  │   │   └── input2::capslock -> ../../devices/platform/e8000000.s_ahb
	//  │   ├── ptp
	//  │   │   └── ptp0             -> ../../devices/pci0000:00/0000:00:1d.0
	//  │   ├── pwm
	//  │   │   └── pwmchip0         -> ../../devices/platform/e8000000.s_ahb
	//  │   ├── rtc
	//  │   │   └── rtc0             -> ../../devices/platform/rtc_cmos/rtc/rtc0
	//  │   └── video4linux
	//  │       └── video0           -> ../../devices/platform/e8000000.s_ahb
	//  └── devices
	//      ├── platform
	//      │   └── e8000000.s_ahb
	//      └── pci0000:00
	//          └──  0000:00:02.0

	tmpdir := c.MkDir()
	for _, dir := range sysFsOverlaysGood {
		err := os.MkdirAll(filepath.Join(tmpdir, "sys", dir), os.ModePerm)
		c.Assert(err, IsNil)
	}

	for _, dir := range sysFsOverlaysBad {
		err := os.MkdirAll(filepath.Join(tmpdir, "sys", dir), os.ModePerm)
		c.Assert(err, IsNil)
	}

	s.testRunPreseedUC20Happy(c, "", tmpdir)
}

func (s *preseedSuite) TestRunPreseedUC20ExecFormatError(c *C) {
	tmpdir := c.MkDir()

	// Mock an exec-format error - the first thing that runUC20PreseedMode
	// does is start snapd in a chroot. So we can override the "chroot"
	// call with a simulated exec format error to simulate the error a
	// user would get when running preseeding on a architecture that is
	// not the image target architecture.
	mockChrootCmd := testutil.MockCommand(c, "chroot", "")
	defer mockChrootCmd.Restore()
	err := os.WriteFile(mockChrootCmd.Exe(), []byte("invalid-exe"), 0755)
	c.Check(err, IsNil)

	popts := &preseed.PreseedCoreOptions{
		CoreOptions: preseed.CoreOptions{
			PrepareImageDir: tmpdir,
		},
		PreseedChrootDir: tmpdir,
	}

	err = preseed.RunUC20PreseedMode(popts)
	c.Check(err, ErrorMatches, `error running snapd, please try installing the "qemu-user-static" package: fork/exec .* exec format error`)
}
