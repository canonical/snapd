package snappy

import (
	"fmt"
	"io/ioutil"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	partition "launchpad.net/snappy/partition"

	. "launchpad.net/gocheck"
)

// Hook up gocheck into the "go test" runner
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
	// setup alternative root for system image
	tempdir := c.MkDir()
	systemImageRoot = tempdir

	makeFakeSystemImageChannelConfig(c, filepath.Join(tempdir, systemImageChannelConfig), "1")
	// setup fake /other partition
	makeFakeSystemImageChannelConfig(c, filepath.Join(tempdir, "other", systemImageChannelConfig), "2")

	// run test webserver instead of talking to the real one
	//
	// The mock webserver versions  "1" and "2"
	s.mockSystemImageWebServer = runMockSystemImageWebServer()
	c.Assert(s.mockSystemImageWebServer, NotNil)

	// create mock system-image-cli
	systemImageCli = makeMockSystemImageCli(c, tempdir)
}

func (s *SITestSuite) TearDownTest(c *C) {
	s.mockSystemImageWebServer.Close()
	systemImageRoot = "/"
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
	c.Assert(parts[0].Name(), Equals, "ubuntu-core")
	c.Assert(parts[0].Version(), Equals, "1")
	c.Assert(parts[0].Hash(), Equals, "e09c13f68fccef3b2fe0f5c8ff5c61acf2173b170b1f2a3646487147690b0970ef6f2c555d7bcb072035f29ee4ea66a6df7f6bb320d358d3a7d78a0c37a8a549")
	c.Assert(parts[0].IsActive(), Equals, true)
	c.Assert(parts[0].Channel(), Equals, "ubuntu-core/devel-proposed")

	// second partition is not active and has a different version
	c.Assert(parts[1].IsActive(), Equals, false)
	c.Assert(parts[1].Version(), Equals, "2")
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
	c.Assert(parts[0].Name(), Equals, "ubuntu-core")
	c.Assert(parts[0].Version(), Equals, "2")
	c.Assert(parts[0].DownloadSize(), Equals, int64(123166488))
}

type MockPartition struct {
	toggleNextBootCalled      bool
	markBootSuccessfulCalled  bool
	syncBootloaderFilesCalled bool
}

func (p *MockPartition) ToggleNextBoot() error {
	p.toggleNextBootCalled = true
	return nil
}

func (p *MockPartition) MarkBootSuccessful() error {
	p.markBootSuccessfulCalled = true
	return nil
}
func (p *MockPartition) SyncBootloaderFiles() error {
	p.syncBootloaderFilesCalled = true
	return nil
}
func (p *MockPartition) IsNextBootOther() bool {
	return false
}

func (p *MockPartition) RunWithOther(option partition.MountOption, f func(otherRoot string) (err error)) (err error) {
	return f("/other")
}

func (s *SITestSuite) TestSystemImagePartInstallUpdatesPartition(c *C) {
	// add a update
	mockSystemImageIndexJSON = fmt.Sprintf(mockSystemImageIndexJSONTemplate, "2")
	parts, err := s.systemImage.Updates()

	sp := parts[0].(*SystemImagePart)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	pb := &MockProgressMeter{}
	// do the install
	err = sp.Install(pb, 0)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBootCalled, Equals, true)
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
	err = sp.Install(pb, 0)
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
	err = sp.Install(nil, 0)

	//
	c.Assert(err.Error(), Equals, fmt.Sprintf("%s failed with return code 1: random\nerror string", systemImageCli))
}

func (s *SITestSuite) TestSystemImagePartInstall(c *C) {
	// add a update
	mockSystemImageIndexJSON = fmt.Sprintf(mockSystemImageIndexJSONTemplate, "2")
	parts, err := s.systemImage.Updates()

	sp := parts[0].(*SystemImagePart)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	err = sp.Install(nil, 0)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBootCalled, Equals, true)
}

func (s *SITestSuite) TestSystemImagePartSetActiveAlreadyActive(c *C) {
	parts, err := s.systemImage.Installed()

	sp := parts[0].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, true)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	err = sp.SetActive()
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBootCalled, Equals, false)
}

func (s *SITestSuite) TestSystemImagePartSetActiveMakeActive(c *C) {
	parts, err := s.systemImage.Installed()

	sp := parts[1].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, false)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	err = sp.SetActive()
	c.Assert(err, IsNil)
	c.Assert(mockPartition.toggleNextBootCalled, Equals, true)
}

func (s *SITestSuite) TestTestVerifyUpgradeWasAppliedSuccess(c *C) {
	// our layout is:
	//  - "1" on current
	//  - "2" on other
	// the webserver will tell us that "2" is latest
	makeFakeSystemImageChannelConfig(c, filepath.Join(systemImageRoot, "other", systemImageChannelConfig), "2")
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
	makeFakeSystemImageChannelConfig(c, filepath.Join(systemImageRoot, "other", systemImageChannelConfig), "1")

	// the update will have "2" and we only installed "1" on other
	parts, err := s.systemImage.Updates()
	part := parts[0].(*SystemImagePart)
	err = part.verifyUpgradeWasApplied()
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, `found latest installed version "1" (expected "2")`)
}

func (s *SITestSuite) TestCannotUninstall(c *C) {
	// whats installed
	parts, err := s.systemImage.Installed()
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 2)

	c.Assert(parts[0].Uninstall(), Equals, ErrPackageNotRemovable)
}
