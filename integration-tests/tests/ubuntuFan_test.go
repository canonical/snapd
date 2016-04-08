// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!lowperformance

/*
 * Copyright (C) 2015 Canonical Ltd
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

package tests

import (
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/wait"

	"gopkg.in/check.v1"
)

const (
	firstOverlaySegment = "241"
	baseContainer       = "busybox"
)

var _ = check.Suite(&fanTestSuite{})

type fanTestSuite struct {
	common.SnappySuite
	bridgeIP  string
	subjectIP string
}

func (s *fanTestSuite) SetUpTest(c *check.C) {
	if common.Release(c) == "15.04" {
		c.Skip("Ubuntu Fan not available in 15.04")
	}

	s.SnappySuite.SetUpTest(c)
	var err error
	s.subjectIP, err = getIPAddr(c)
	c.Assert(err, check.IsNil, check.Commentf("Error getting IP address: %s", err))

	s.fanCtl(c, "up")
	s.bridgeIP = s.fanBridgeIP(c)
}

func (s *fanTestSuite) TearDownTest(c *check.C) {
	s.SnappySuite.TearDownTest(c)

	s.fanCtl(c, "down")
}

func (s *fanTestSuite) TestFanCommandExists(c *check.C) {
	cmd := exec.Command("fanctl")
	output, _ := cmd.CombinedOutput()

	expectedPattern := `(?msi)Usage: \/usr\/sbin\/fanctl <cmd>.*`

	c.Assert(string(output), check.Matches, expectedPattern,
		check.Commentf("Expected output pattern %s not found in %s", expectedPattern, output))
}

func (s *fanTestSuite) TestFanCommandCreatesFanBridge(c *check.C) {
	output := cli.ExecCommand(c, "ifconfig")

	expectedPattern := fmt.Sprintf("(?msi).*%s.*%s.*", s.fanName(), s.bridgeIP)

	c.Assert(output, check.Matches, expectedPattern,
		check.Commentf("Expected pattern %s not found in %s", expectedPattern, output))
}

func (s *fanTestSuite) TestDockerCreatesAContainerInsideTheFan(c *check.C) {
	c.Skip("Skipping until LP: #1544507 is fixed")

	setUpDocker(c)
	defer tearDownDocker(c)
	s.configureDockerToUseBridge(c)
	defer s.removeBridgeFromDockerConf(c)

	output := cli.ExecCommand(c, "docker", "run", "-t", baseContainer, "ifconfig")

	expectedIP := strings.TrimRight(s.bridgeIP, ".1") + ".2"
	expectedPattern := fmt.Sprintf("(?ms).*inet addr:%s.*", expectedIP)

	c.Assert(output, check.Matches, expectedPattern,
		check.Commentf("Expected pattern %s not found in %s", expectedPattern, output))
}

func (s *fanTestSuite) TestContainersInTheFanAreReachable(c *check.C) {
	c.Skip("Skipping until LP: #1544507 is fixed")

	setUpDocker(c)
	defer tearDownDocker(c)
	s.configureDockerToUseBridge(c)
	defer s.removeBridgeFromDockerConf(c)

	// spin up first container
	cli.ExecCommand(c, "docker", "run", "-d", "-t", baseContainer)
	// the first assigned IP in the fan will end with ".2"
	firstIPAddr := strings.TrimRight(s.bridgeIP, ".1") + ".2"

	// ping from a second container
	output := cli.ExecCommand(c, "docker", "run", "-t", baseContainer, "ping", firstIPAddr, "-c", "1")

	expectedPattern := "(?ms).*1 packets transmitted, 1 packets received, 0% packet loss.*"

	c.Assert(output, check.Matches, expectedPattern,
		check.Commentf("Expected pattern %s not found in %s", expectedPattern, output))
}

func getIPAddr(c *check.C) (ip string, err error) {
	if ips, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range ips {
			hostport := addr.String()
			// poor's man check for ipv6
			if !strings.Contains(hostport, ":") &&
				!strings.Contains(hostport, "127.0.0.1") {
				parts := strings.Split(hostport, "/")
				return parts[0], nil
			}
		}
	}
	return
}

func (s *fanTestSuite) fanBridgeIP(c *check.C) (bridgeIP string) {
	segments := strings.Split(s.subjectIP, ".")

	// the final bridge ip is formed by the given overlay first segment, the last two from
	// the interface ip and "1" at the end
	return strings.Join([]string{firstOverlaySegment, segments[2], segments[3], "1"}, ".")
}

func (s *fanTestSuite) fanCtl(c *check.C, cmd string) string {
	return cli.ExecCommand(c,
		"sudo", "fanctl", cmd, firstOverlaySegment+".0.0.0/8", s.subjectIP+"/16")
}

func (s *fanTestSuite) configureDockerToUseBridge(c *check.C) {
	cfgFile := dockerCfgFile(c)

	cli.ExecCommand(c, "sudo", "sed", "-i",
		fmt.Sprintf(`s/DOCKER_OPTIONS=\"\"/DOCKER_OPTIONS=\"%s\"/`, s.dockerOptions()),
		cfgFile)

	restartDocker(c)
}

func (s *fanTestSuite) removeBridgeFromDockerConf(c *check.C) {
	cfgFile := dockerCfgFile(c)

	cli.ExecCommand(c, "sudo", "sed", "-i",
		`s/DOCKER_OPTIONS=\".*\"/DOCKER_OPTIONS=\"\"/`,
		cfgFile)

	restartDocker(c)
}

func dockerCfgFile(c *check.C) string {
	dockerVersion := common.GetCurrentVersion(c, "docker")
	return fmt.Sprintf("/var/snap/docker/%s/etc/docker.conf", dockerVersion)
}

func restartDocker(c *check.C) {
	dockerVersion := common.GetCurrentVersion(c, "docker")
	dockerService := fmt.Sprintf("docker_docker-daemon_%s.service", dockerVersion)

	cli.ExecCommand(c, "sudo", "systemctl", "restart", dockerService)

	// we need to wait until the socket is ready, an active systemctl status is not enough
	err := wait.ForActiveService(c, dockerService)
	c.Assert(err, check.IsNil, check.Commentf("Expected nil error, got %s", err))

	err = wait.ForCommand(c, `(?ms).*docker\.sock\s.*`, "ls", "/run")
	c.Assert(err, check.IsNil, check.Commentf("Expected nil error, got %s", err))
}

func (s *fanTestSuite) fanName() string {
	firstOctect := strings.Split(s.bridgeIP, ".")[0]

	return "fan-" + firstOctect
}

func (s *fanTestSuite) dockerOptions() string {
	return fmt.Sprintf("-d -b %s --mtu=1480 --iptables=false", s.fanName())
}

func setUpDocker(c *check.C) {
	common.InstallSnap(c, "docker/edge")
	dockerVersion := common.GetCurrentVersion(c, "docker")
	dockerService := fmt.Sprintf("docker_docker-daemon_%s.service", dockerVersion)

	err := wait.ForActiveService(c, dockerService)
	c.Assert(err, check.IsNil, check.Commentf("Error waiting for service: %s", err))

	err = wait.ForCommand(c, `(?ms).*docker\.sock\s.*`, "ls", "/run")
	c.Assert(err, check.IsNil, check.Commentf("Expected nil error, got %s", err))

	cli.ExecCommand(c, "docker", "pull", baseContainer)
}

func tearDownDocker(c *check.C) {
	common.RemoveSnap(c, "docker")
}
