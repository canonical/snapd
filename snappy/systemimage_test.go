// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"fmt"
	"io/ioutil"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/partition"
	"github.com/ubuntu-core/snappy/provisioning"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type SITestSuite struct {
	systemImage              *SystemImageRepository
	mockSystemImageWebServer *httptest.Server
}

var _ = Suite(&SITestSuite{})

func (s *SITestSuite) SetUpTest(c *C) {
	newPartition = func() (p partition.Interface) {
		return new(MockPartition)
	}

	s.systemImage = NewSystemImageRepository()
	c.Assert(s, NotNil)

	makeFakeSystemImageChannelConfig(c, filepath.Join(dirs.GlobalRootDir, systemImageChannelConfig), "1")
	// setup fake /other partition
	makeFakeSystemImageChannelConfig(c, filepath.Join(dirs.GlobalRootDir, "other", systemImageChannelConfig), "0")

	// run test webserver instead of talking to the real one
	//
	// The mock webserver versions  "1" and "2"
	s.mockSystemImageWebServer = runMockSystemImageWebServer()
	c.Assert(s.mockSystemImageWebServer, NotNil)

	// create mock system-image-cli
	systemImageCli = makeMockSystemImageCli(c, dirs.GlobalRootDir)
}

func (s *SITestSuite) TearDownTest(c *C) {
	s.mockSystemImageWebServer.Close()
	bootloaderDir = bootloaderDirImpl
}

func makeMockSystemImageCli(c *C, tempdir string) string {
	s := `#!/bin/sh

printf '{"type": "progress", "now": 20, "total":100}\n'
printf '{"type": "progress", "now": 40, "total":100}\n'
printf '{"type": "progress", "now": 60, "total":100}\n'
printf '{"type": "progress", "now": 80, "total":100}\n'
printf '{"type": "progress", "now": 100, "total":100}\n'
printf '{"type": "spinner", "msg": "Applying"}\n'
`
	mockScript := filepath.Join(tempdir, "system-image-cli")
	err := ioutil.WriteFile(mockScript, []byte(s), 0755)
	c.Assert(err, IsNil)

	return mockScript
}

func makeFakeSystemImageChannelConfig(c *C, cfgPath, buildNumber string) {
	os.MkdirAll(filepath.Dir(cfgPath), 0775)
	f, err := os.OpenFile(cfgPath, os.O_CREATE|os.O_RDWR, 0664)
	c.Assert(err, IsNil)
	defer f.Close()
	f.Write([]byte(fmt.Sprintf(`
[service]
base: system-image.ubuntu.com
http_port: 80
https_port: 443
channel: ubuntu-core/devel-proposed
device: generic_amd64
build_number: %s
version_detail: ubuntu=20141206,raw-device=20141206,version=77
`, buildNumber)))
}

func (s *SITestSuite) TestTestInstalled(c *C) {
	// whats installed
	parts, err := s.systemImage.Installed()
	c.Assert(err, IsNil)
	// we have one active and one inactive
	c.Assert(parts, HasLen, 2)
	c.Assert(parts[0].Name(), Equals, SystemImagePartName)
	c.Assert(parts[0].Origin(), Equals, SystemImagePartOrigin)
	c.Assert(parts[0].Vendor(), Equals, SystemImagePartVendor)
	c.Assert(parts[0].Version(), Equals, "1")
	c.Assert(parts[0].Hash(), Equals, "e09c13f68fccef3b2fe0f5c8ff5c61acf2173b170b1f2a3646487147690b0970ef6f2c555d7bcb072035f29ee4ea66a6df7f6bb320d358d3a7d78a0c37a8a549")
	c.Assert(parts[0].IsActive(), Equals, true)
	c.Assert(parts[0].Channel(), Equals, "ubuntu-core/devel-proposed")

	// second partition is not active and has a different version
	c.Assert(parts[1].IsActive(), Equals, false)
	c.Assert(parts[1].Version(), Equals, "0")
}

func (s *SITestSuite) TestUpdateNoUpdate(c *C) {
	mockSystemImageIndexJSON = fmt.Sprintf(mockSystemImageIndexJSONTemplate, "1")
	parts, err := s.systemImage.Updates()
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 0)
}

func (s *SITestSuite) TestUpdateHasUpdate(c *C) {
	// add a update
	mockSystemImageIndexJSON = fmt.Sprintf(mockSystemImageIndexJSONTemplate, "2")
	parts, err := s.systemImage.Updates()
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 1)
	c.Assert(parts[0].Name(), Equals, SystemImagePartName)
	c.Assert(parts[0].Version(), Equals, "2")
	c.Assert(parts[0].DownloadSize(), Equals, int64(123166488))
}

type MockPartition struct {
	toggleNextBoot            bool
	markBootSuccessfulCalled  bool
	syncBootloaderFilesCalled bool
}

func (p *MockPartition) ToggleNextBoot() error {
	p.toggleNextBoot = !p.toggleNextBoot
	return nil
}

func (p *MockPartition) MarkBootSuccessful() error {
	p.markBootSuccessfulCalled = true
	return nil
}
func (p *MockPartition) SyncBootloaderFiles(map[string]string) error {
	p.syncBootloaderFilesCalled = true
	return nil
}
func (p *MockPartition) IsNextBootOther() bool {
	return p.toggleNextBoot
}

func (p *MockPartition) RunWithOther(option partition.MountOption, f func(otherRoot string) (err error)) (err error) {
	return f("/other")
}

// used by GetBootLoaderDir(), used to test sideload logic.
var tempBootDir string

func (p *MockPartition) BootloaderDir() string {
	return tempBootDir
}

func (s *SITestSuite) TestSystemImagePartInstallUpdatesPartition(c *C) {
	// FIXME: ideally we would change the version to "2" as a side-effect
	// of calling sp.Install() we need to update it because the
	// sp.Install() will verify that it got applied
	makeFakeSystemImageChannelConfig(c, filepath.Join(dirs.GlobalRootDir, "other", systemImageChannelConfig), "2")

	// add a update
	mockSystemImageIndexJSON = fmt.Sprintf(mockSystemImageIndexJSONTemplate, "2")
	parts, err := s.systemImage.Updates()
	c.Assert(err, IsNil)

	sp := parts[0].(*SystemImagePart)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	pb := &MockProgressMeter{}
	// do the install
	_, err = sp.Install(pb, 0)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBoot, Equals, true)
	c.Assert(pb.total, Equals, 100.0)
	c.Assert(pb.spin, Equals, true)
	c.Assert(pb.spinMsg, Equals, "Applying")
	c.Assert(pb.finished, Equals, true)
	c.Assert(pb.progress, DeepEquals, []float64{20.0, 40.0, 60.0, 80.0, 100.0})
}

func (s *SITestSuite) TestSystemImagePartInstallUpdatesBroken(c *C) {
	// fake a broken upgrade
	scriptContent := `#!/bin/sh
printf '{"type": "error", "msg": "some error msg"}\n'
`
	err := ioutil.WriteFile(systemImageCli, []byte(scriptContent), 0755)
	c.Assert(err, IsNil)

	// add a update
	mockSystemImageIndexJSON = fmt.Sprintf(mockSystemImageIndexJSONTemplate, "2")
	parts, err := s.systemImage.Updates()

	sp := parts[0].(*SystemImagePart)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	pb := &MockProgressMeter{}
	// do the install
	_, err = sp.Install(pb, 0)
	c.Assert(strings.HasSuffix(err.Error(), "some error msg"), Equals, true)
}

func (s *SITestSuite) TestSystemImagePartInstallUpdatesCrashes(c *C) {
	scriptContent := `#!/bin/sh
printf "random\nerror string" >&2
exit 1
`
	err := ioutil.WriteFile(systemImageCli, []byte(scriptContent), 0755)
	c.Assert(err, IsNil)

	// add a update
	mockSystemImageIndexJSON = fmt.Sprintf(mockSystemImageIndexJSONTemplate, "2")
	parts, err := s.systemImage.Updates()

	sp := parts[0].(*SystemImagePart)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	// do the install and pretend something goes wrong
	_, err = sp.Install(nil, 0)

	//
	c.Assert(err.Error(), Equals, fmt.Sprintf("%s failed with return code 1: random\nerror string", systemImageCli))
}

func (s *SITestSuite) TestSystemImagePartInstall(c *C) {

	// FIXME: ideally we would change the version to "2" as a side-effect
	// of calling sp.Install() we need to update it because the
	// sp.Install() will verify that it got applied
	makeFakeSystemImageChannelConfig(c, filepath.Join(dirs.GlobalRootDir, "other", systemImageChannelConfig), "2")

	// add a update
	mockSystemImageIndexJSON = fmt.Sprintf(mockSystemImageIndexJSONTemplate, "2")
	parts, err := s.systemImage.Updates()

	sp := parts[0].(*SystemImagePart)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	_, err = sp.Install(nil, 0)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBoot, Equals, true)
}

func (s *SITestSuite) TestSystemImagePartSetActiveAlreadyActive(c *C) {
	parts, err := s.systemImage.Installed()

	sp := parts[0].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, true)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	err = sp.SetActive(true, nil)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBoot, Equals, false)
}

func (s *SITestSuite) TestSystemImagePartSetActiveMakeActive(c *C) {
	parts, err := s.systemImage.Installed()

	sp := parts[1].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, false)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	err = sp.SetActive(true, nil)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBoot, Equals, true)
}

func (s *SITestSuite) TestSystemImagePartSetActiveAlreadyToggled(c *C) {
	// in other words, check that calling SetActive twice does not
	// untoggle
	parts, err := s.systemImage.Installed()

	sp := parts[1].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, false)
	mockPartition := MockPartition{toggleNextBoot: true}
	sp.partition = &mockPartition

	err = sp.SetActive(true, nil)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBoot, Equals, true)
}

func (s *SITestSuite) TestSystemImagePartSetActiveAlreadyActiveAlreadyToggled(c *C) {
	// in other words, check that calling SetActive on one
	// partition when it's been called on the other one undoes the
	// toggle
	parts, err := s.systemImage.Installed()

	sp := parts[0].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, true)
	mockPartition := MockPartition{toggleNextBoot: true}
	sp.partition = &mockPartition

	err = sp.SetActive(true, nil)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBoot, Equals, false)
}

func (s *SITestSuite) TestSystemImagePartUnsetActiveOnActive(c *C) {
	parts, err := s.systemImage.Installed()

	sp := parts[0].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, true)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	err = sp.SetActive(false, nil)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBoot, Equals, true)
}

func (s *SITestSuite) TestSystemImagePartUnsetActiveOnUnactiveNOP(c *C) {
	parts, err := s.systemImage.Installed()

	sp := parts[1].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, false)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	err = sp.SetActive(false, nil)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBoot, Equals, false)
}

func (s *SITestSuite) TestSystemImagePartUnsetActiveAlreadyToggled(c *C) {
	parts, err := s.systemImage.Installed()

	sp := parts[1].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, false)
	mockPartition := MockPartition{toggleNextBoot: true}
	sp.partition = &mockPartition

	err = sp.SetActive(false, nil)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBoot, Equals, false)
}

func (s *SITestSuite) TestSystemImagePartUnsetActiveAlreadyActiveAlreadyToggled(c *C) {
	parts, err := s.systemImage.Installed()

	sp := parts[0].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, true)
	mockPartition := MockPartition{toggleNextBoot: true}
	sp.partition = &mockPartition

	err = sp.SetActive(false, nil)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBoot, Equals, true)
}

func (s *SITestSuite) TestTestVerifyUpgradeWasAppliedSuccess(c *C) {
	// our layout is:
	//  - "1" on current
	//  - "2" on other
	// the webserver will tell us that "2" is latest
	makeFakeSystemImageChannelConfig(c, filepath.Join(dirs.GlobalRootDir, "other", systemImageChannelConfig), "2")
	parts, err := s.systemImage.Updates()

	part := parts[0].(*SystemImagePart)
	err = part.verifyUpgradeWasApplied()
	c.Assert(err, IsNil)
}

func (s *SITestSuite) TestTestVerifyUpgradeWasAppliedFailure(c *C) {
	// see TestTestVerifyUpgradeWasAppliedSuccess
	//
	// but this time the other part is *not* updated, i.e. we set it to
	// something else
	makeFakeSystemImageChannelConfig(c, filepath.Join(dirs.GlobalRootDir, "other", systemImageChannelConfig), "1")

	// the update will have "2" and we only installed "1" on other
	parts, err := s.systemImage.Updates()
	part := parts[0].(*SystemImagePart)
	err = part.verifyUpgradeWasApplied()
	c.Assert(err, NotNil)
	_, isErrUpgradeVerificationFailed := err.(*ErrUpgradeVerificationFailed)
	c.Assert(isErrUpgradeVerificationFailed, Equals, true)
	c.Assert(err.Error(), Equals, `upgrade verification failed: found "1" but expected "2"`)
}

func (s *SITestSuite) TestOtherIsEmpty(c *C) {
	otherRoot := "/other"
	otherRootFull := filepath.Join(dirs.GlobalRootDir, otherRoot)

	siConfig := filepath.Join(otherRootFull, systemImageChannelConfig)

	// the tests create si-config files for "current" and "other"
	c.Assert(otherIsEmpty(otherRoot), Equals, false)

	// make the siConfig zero bytes (as is done by the upgrader when
	// first populating "other" to denote that the update is in
	// progress.
	err := ioutil.WriteFile(siConfig, []byte(""), 0640)
	c.Assert(err, IsNil)
	c.Assert(otherIsEmpty(otherRoot), Equals, true)

	err = ioutil.WriteFile(siConfig, []byte("\n"), 0640)
	c.Assert(err, IsNil)
	c.Assert(otherIsEmpty(otherRoot), Equals, false)

	err = ioutil.WriteFile(siConfig, []byte("foo"), 0640)
	c.Assert(err, IsNil)
	c.Assert(otherIsEmpty(otherRoot), Equals, false)

	os.Remove(siConfig)
	c.Assert(otherIsEmpty(otherRoot), Equals, true)
}

func (s *SITestSuite) TestCannotUninstall(c *C) {
	// whats installed
	parts, err := s.systemImage.Installed()
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 2)

	c.Assert(parts[0].Uninstall(nil), Equals, ErrPackageNotRemovable)
}

func (s *SITestSuite) TestFrameworks(c *C) {
	parts, err := s.systemImage.Installed()
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 2)
	fmks, err := parts[0].Frameworks()
	c.Assert(err, IsNil)
	c.Check(fmks, HasLen, 0)
}

func (s *SITestSuite) TestOrigin(c *C) {
	parts, err := s.systemImage.Installed()
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 2)
	c.Assert(parts[0].Origin(), Equals, SystemImagePartOrigin)
	c.Assert(parts[1].Origin(), Equals, SystemImagePartOrigin)
}

func (s *SITestSuite) TestCannotUpdateIfSideLoaded(c *C) {
	var err error
	var yamlData = `
meta:
  timestamp: 2015-04-20T14:15:39.013515821+01:00
  initial-revision: r345
  system-image-server: http://system-image.ubuntu.com

tool:
  name: ubuntu-device-flash
  path: /usr/bin/ubuntu-device-flash
  version: ""

options:
  size: 3
  size-unit: GB
  output: /tmp/bbb.img
  channel: ubuntu-core/devel-proposed
  device-part: /some/path/file.tgz
  developer-mode: true
`
	tempBootDir := c.MkDir()
	parts, err := s.systemImage.Updates()

	sp := parts[0].(*SystemImagePart)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	bootloaderDir = func() string { return tempBootDir }

	sideLoaded := filepath.Join(tempBootDir, provisioning.InstallYamlFile)

	err = os.MkdirAll(filepath.Dir(sideLoaded), 0775)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(sideLoaded, []byte(yamlData), 0640)
	c.Assert(err, IsNil)

	pb := &MockProgressMeter{}

	// Ensure the install fails if the system is sideloaded
	_, err = sp.Install(pb, 0)
	c.Assert(err, Equals, ErrSideLoaded)
}

// These are regression tests for #1474125 - we do not want to sync the
// bootfiles on a upgrade->rollback->upgrade
//
// Let:
// - upgrade from ubuntu-core
//    v1 (on a with kernel k1) -> v2 (ob b with kernel k2)
//   now we have: /boot/a/k1 /boot/b/k2
// - boot into "b" and rollback from v2(on b) -> v1 (on a)
//   we still have: /boot/a/k1 /boot/b/k2
// - upgrade from ubuntu-core v1 (on a) -> v2 (ob b)
//   syncbootfiles is run and it will copy: /boot/a/k1 -> /boot/b/
//   *but* v2 is already on the other partition so snappy does
//   not actually download/install anything so we end up with
//   the wrong kernel /boot/b/k1
//
// see bug https://bugs.launchpad.net/snappy/+bug/1474125
func (s *SITestSuite) TestSystemImagePartInstallRollbackNoSyncbootfiles(c *C) {

	// we are on 1 and "upgrade" to 2 which is already installed
	// (e.g. because we rolled back earlier)
	makeFakeSystemImageChannelConfig(c, filepath.Join(dirs.GlobalRootDir, systemImageChannelConfig), "1")
	makeFakeSystemImageChannelConfig(c, filepath.Join(dirs.GlobalRootDir, "other", systemImageChannelConfig), "2")

	// now we get the other part (v2)
	parts, err := s.systemImage.Installed()
	c.Assert(err, IsNil)
	sp := parts[1].(*SystemImagePart)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	// and install it (but its already installed so system-image-cli
	// will not download anything)
	_, err = sp.Install(&MockProgressMeter{}, 0)
	c.Assert(err, IsNil)

	// ensure that we do not sync the bootfiles in this case (see above)
	c.Assert(mockPartition.syncBootloaderFilesCalled, Equals, false)
	c.Assert(mockPartition.toggleNextBoot, Equals, true)
}

func (s *SITestSuite) TestNeedsBootAssetSyncNeedsSync(c *C) {
	parts, err := s.systemImage.Updates()
	c.Assert(err, IsNil)
	part := parts[0].(*SystemImagePart)

	c.Assert(part.needsBootAssetSync(), Equals, true)
}

func (s *SITestSuite) TestNeedsBootAssetSyncNoNeed(c *C) {
	makeFakeSystemImageChannelConfig(c, filepath.Join(dirs.GlobalRootDir, "other", systemImageChannelConfig), "2")

	parts, err := s.systemImage.Installed()
	c.Assert(err, IsNil)
	part := parts[0].(*SystemImagePart)

	c.Assert(part.needsBootAssetSync(), Equals, false)
}
