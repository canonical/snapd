package tests

import . "gopkg.in/check.v1"

var _ = Suite(&InstallSuite{})

type InstallSuite struct {
	CommonSuite
}

func installSnap(c *C, packageName string) []byte {
	return execCommand(c, "sudo", "snappy", "install", packageName)
}

func (s *InstallSuite) TearDownTest(c *C) {
	execCommand(c, "sudo", "snappy", "remove", "hello-world")
}

func (s *InstallSuite) TestInstallSnapMustPrintPackageInformation(c *C) {
	installOutput := installSnap(c, "hello-world")

	expected := "" +
		"Installing hello-world\n" +
		"Name          Date       Version Developer \n" +
		".*\n" +
		"hello-world   .* .*  canonical \n" +
		".*\n"
	c.Assert(string(installOutput), Matches, expected)
}

func (s *InstallSuite) TestCallBinaryFromInstalledSnap(c *C) {
	installSnap(c, "hello-world")

	echoOutput := execCommand(c, "hello-world.echo")

	c.Assert(string(echoOutput), Equals, "Hello World!\n")
}

func (s *InstallSuite) TestInfoMustPrintInstalledPackageInformation(c *C) {
	installSnap(c, "hello-world")

	infoOutput := execCommand(c, "sudo", "snappy", "info")

	expected := "(?ms).*^apps: hello-world\n"
	c.Assert(string(infoOutput), Matches, expected)
}
