package tests

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v2"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/provisioning"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&InstallSuite{})

type InstallSuite struct{}

func (s *InstallSuite) installSnap(c *C, packageName string) []byte {
	return execCommand(c, "sudo", "snappy", "install", packageName)
}

func execCommand(c *C, cmds ...string) []byte {
	cmd := exec.Command(cmds[0], cmds[1:len(cmds)]...)
	output, err := cmd.CombinedOutput()
	c.Assert(err, IsNil, Commentf("Error: %v", output))
	return output
}

func (s *InstallSuite) SetUpSuite(c *C) {
	execCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
}

func (s *InstallSuite) TearDownTest(c *C) {
	execCommand(c, "sudo", "snappy", "remove", "hello-world")
}

func (s *InstallSuite) TestInstallSnapMustPrintPackageInformation(c *C) {
	installOutput := s.installSnap(c, "hello-world")

	expected := "" +
		"Installing hello-world\n" +
		"Name          Date       Version Developer \n" +
		".*\n" +
		"hello-world   .* .*  canonical \n" +
		".*\n"
	c.Assert(string(installOutput), Matches, expected)
}

func (s *InstallSuite) TestCallBinaryFromInstalledSnap(c *C) {
	s.installSnap(c, "hello-world")

	echoOutput := execCommand(c, "hello-world.echo")

	c.Assert(string(echoOutput), Equals, "Hello World!\n")
}

func (s *InstallSuite) TestInfoMustPrintInstalledPackageInformation(c *C) {
	s.installSnap(c, "hello-world")

	infoOutput := execCommand(c, "sudo", "snappy", "info")

	expected := "(?ms).*^apps: hello-world\n"
	c.Assert(string(infoOutput), Matches, expected)
}

var _ = Suite(&UpdateSuite{})

type UpdateSuite struct {
	installYamlPath string
}

const (
	grub  = "/boot/grub"
	uboot = "/boot/uboot"
)

// this function is a hack until we unify bootloader code paths
func installYamlPath() string {
	for _, bootPath := range []string{grub, uboot} {
		yamlPath := filepath.Join(bootPath, provisioning.InstallYamlFile)
		if helpers.FileExists(yamlPath) {
			return yamlPath
		}
	}

	return ""
}

func writeInstallYaml(installYamlPath string, installYaml provisioning.InstallYaml, c *C) {
	yamlContent, err := yaml.Marshal(&installYaml)
	c.Assert(err, IsNil)
	c.Assert(ioutil.WriteFile(installYamlPath, yamlContent, 0444), IsNil)
}

func readInstallYaml(installYamlPath string, c *C) provisioning.InstallYaml {
	yamlContent, err := ioutil.ReadFile(installYamlPath)
	c.Assert(err, IsNil)

	var installYaml provisioning.InstallYaml
	c.Assert(yaml.Unmarshal(yamlContent, &installYaml), IsNil)

	return installYaml
}

func (s *UpdateSuite) SetUpSuite(c *C) {
	execCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
	s.installYamlPath = installYamlPath()
	if s.installYamlPath != "" {
		c.Assert(helpers.CopyFile(s.installYamlPath, s.installYamlPath+".orig", helpers.CopyFlagSync), IsNil, "Cannot backup install.yaml")
	}
}

func (s *UpdateSuite) TearDownTest(c *C) {
	if s.installYamlPath != "" {
		c.Assert(os.Rename(s.installYamlPath+".orig", s.installYamlPath), IsNil, "Cannot restore install.yaml")
	}
}

func (s *UpdateSuite) TestDoNotUpdateSideloadedOS(c *C) {
	if s.installYamlPath == "" {
		c.Skip("Unsupported system, no install yaml")
	}

	installYaml := readInstallYaml(s.installYamlPath, c)
	installYaml.InstallOptions.DevicePart = "somedevice.tar.xz"
	writeInstallYaml(s.installYamlPath, installYaml, c)

	output := execCommand(c, "sudo", "snappy", "update")
	c.Check(output, Matches, ".*Skipping sideloaded package: ubuntu-core.*")
}
