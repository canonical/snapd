package tests

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	. "gopkg.in/check.v1"
)

type FailoverSuite struct {
	CommonSuite
}

var _ = Suite(&FailoverSuite{})

const (
	baseOtherPath             = "/writable/cache/system"
	origKernelfilenamePattern = "boot/%svmlinuz*"
	destKernelFilenamePrefix  = "snappy-selftest-"
	channelCfgFile            = "/etc/system-image/channel.ini"
)

type failover interface {
	set(c *C)
	unset(c *C)
}

type ZeroSizeKernel struct{}

func (zsk ZeroSizeKernel) set(c *C) {
	makeWritable(c, baseOtherPath)

	completePattern := filepath.Join(
		baseOtherPath,
		fmt.Sprintf(origKernelfilenamePattern, ""))
	oldKernelFilename := getSingleFilename(c, completePattern)
	newKernelFilename := fmt.Sprintf(
		"%s/%s%s", baseOtherPath, destKernelFilenamePrefix, oldKernelFilename)

	err := os.Rename(oldKernelFilename, newKernelFilename)
	c.Assert(err, IsNil,
		Commentf("Error renaming file %s to %s", oldKernelFilename, newKernelFilename))
}

func (zsk ZeroSizeKernel) unset(c *C) {
	makeWritable(c, baseOtherPath)

	completePattern := filepath.Join(
		baseOtherPath,
		fmt.Sprintf(origKernelfilenamePattern, destKernelFilenamePrefix))
	oldKernelFilename := getSingleFilename(c, completePattern)
	newKernelFilename := strings.Replace(oldKernelFilename, destKernelFilenamePrefix, "", 1)

	err := os.Rename(oldKernelFilename, newKernelFilename)
	c.Assert(err, IsNil,
		Commentf("Error renaming file %s to %s", oldKernelFilename, newKernelFilename))
}

func (s *FailoverSuite) TestZeroSizeKernel(c *C) {
	commonFailoverTest(c, ZeroSizeKernel{})
}

func commonFailoverTest(c *C, f failover) {
	currentVersion := getCurrentVersion(c)

	if afterReboot(c) {
		f.unset(c)
		c.Assert(getSavedVersion(c), Equals, currentVersion)
	} else {
		switchChannelVersion(c, currentVersion, currentVersion-1)
		setSavedVersion(c, currentVersion)

		callUpdate(c)
		f.set(c)
		reboot(c)
	}
}

func reboot(c *C) {
	// This will write the name of the current test as a reboot mark
	execCommand(c, "sudo", "/tmp/autopkgtest-reboot", c.TestName())
}

func afterReboot(c *C) bool {
	// $ADT_REBOOT_MARK contains the reboot mark, if we have rebooted it'll be the test name
	return os.Getenv("ADT_REBOOT_MARK") == c.TestName()
}

func getCurrentVersion(c *C) int {
	output := execCommand(c, "snappy", "list")
	pattern := "(?mU)^ubuntu-core (.*)$"
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(string(output))
	c.Assert(match, NotNil, Commentf("Version not found in %s", output))

	// match is like "ubuntu-core   2015-06-18 93        ubuntu"
	items := strings.Split(match[0], " ")
	version, err := strconv.Atoi(items[2])
	c.Assert(err, IsNil, "Error converting version to int %v", version)
	return version
}

func setSavedVersion(c *C, version int) {
	versionFile := getVersionFile()
	err := ioutil.WriteFile(versionFile, []byte(strconv.Itoa(version)), 0777)
	c.Assert(err, IsNil, Commentf("Error writing version file %s with %s", versionFile, version))
}

func getSavedVersion(c *C) int {
	versionFile := getVersionFile()
	contents, err := ioutil.ReadFile(versionFile)
	c.Assert(err, IsNil, Commentf("Error reading version file %s", versionFile))
	version, err := strconv.Atoi(string(contents))
	c.Assert(err, IsNil, Commentf("Error converting version %v", contents))
	return version
}

func getVersionFile() string {
	return filepath.Join(os.Getenv("ADT_ARTIFACTS"), "version")
}

func switchChannelVersion(c *C, oldVersion, newVersion int) {
	targets := []string{"/", baseOtherPath}
	for _, target := range targets {
		makeWritable(c, target)
		execCommand(c,
			fmt.Sprintf(
				"sed -i 's/build_number: %d/build_number: %d/' %s",
				oldVersion,
				newVersion,
				filepath.Join(target, channelCfgFile)))
		makeReadonly(c, target)
	}
}

func callUpdate(c *C) {
	execCommand(c, "sudo", "snappy", "update")
}

func makeWritable(c *C, path string) {
	execCommand(c, "sudo", "mount", "-o", "remount,rw", path)
}

func makeReadonly(c *C, path string) {
	execCommand(c, "sudo", "mount", "-o", "remount,ro", path)
}

func getSingleFilename(c *C, pattern string) string {
	matches, err := filepath.Glob(pattern)

	c.Assert(err, IsNil, Commentf("Error: %v", err))
	c.Check(len(matches), Equals, 1)

	return matches[0]
}
