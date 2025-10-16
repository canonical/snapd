// -*- Mode: Go; indent-tabs-mode: t -*-

//go:build clustering

/*
 * Copyright (C) 2025 Canonical Ltd
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

package mdns_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/snapcore/snapd/cluster/mdns"
	"github.com/snapcore/snapd/osutil"
	"gopkg.in/check.v1"
)

const (
	discoverIfaceName        = "mdnspeer0"
	discoverUserNamespaceEnv = "SNAPD_MDNS_DISCOVER_USERNS"
)

func Test(t *testing.T) { check.TestingT(t) }

var _ = check.Suite(&discoverSuite{})

type discoverSuite struct{}

// TestMain re-executes the tests in this package inside a dedicated user and
// network namespace. The helper gains root-equivalent privileges there before
// setting up the interfaces required by the suite.
func TestMain(m *testing.M) {
	if !osutil.GetenvBool(discoverUserNamespaceEnv) {
		code, err := runTestsInUserNamespace()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mdns discovery tests: cannot enter user namespace: %v\n", err)
			os.Exit(1)
		}

		os.Exit(code)
	}

	os.Exit(m.Run())
}

func setupNetworkForTest(c *check.C) (restore func()) {
	if os.Getuid() != 0 {
		c.Skip("mdns discovery tests require root privileges")
	}

	const (
		discoverHostLinkName = "mdnshost0"
		discoverHostCIDR     = "192.0.2.1/24"
		discoverIfaceCIDR    = "192.0.2.2/24"
	)

	teardown := [][]string{
		{"ip", "link", "del", discoverHostLinkName},
	}

	executeIgnoreErrors(teardown)

	setup := [][]string{
		{"ip", "link", "add", discoverHostLinkName, "type", "veth", "peer", "name", discoverIfaceName},
		{"ip", "link", "set", discoverHostLinkName, "up"},
		{"ip", "addr", "add", discoverHostCIDR, "dev", discoverHostLinkName},
		{"ip", "link", "set", discoverIfaceName, "up"},
		{"ip", "addr", "add", discoverIfaceCIDR, "dev", discoverIfaceName},
	}

	if err := execute(setup); err != nil {
		executeIgnoreErrors(teardown)
		c.Fatalf("cannot setup network namespace for discovery tests: %v", err)
	}

	return func() {
		executeIgnoreErrors(teardown)
	}
}

func runTestsInUserNamespace() (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("cannot locate test executable: %w", err)
	}

	args := append([]string{"--user", "--map-root-user", "-n", exe}, os.Args[1:]...)
	cmd := exec.Command("unshare", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), discoverUserNamespaceEnv+"=1")

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}

		return 0, fmt.Errorf("cannot re-exec mdns discovery tests inside user namespace: %w", err)
	}

	return 0, nil
}

func execute(cmds [][]string) error {
	for _, args := range cmds {
		if len(args) == 0 {
			continue
		}

		cmd := exec.Command(args[0], args[1:]...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot execute %q: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
		}
	}

	return nil
}

func executeIgnoreErrors(cmds [][]string) {
	for _, args := range cmds {
		if len(args) == 0 {
			continue
		}

		cmd := exec.Command(args[0], args[1:]...)
		cmd.Run()
	}
}

func (s *discoverSuite) TestMulticastDiscovery(c *check.C) {
	restore := setupNetworkForTest(c)
	defer restore()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	const (
		port            = 8080
		serviceName     = "snapd-self"
		discoverIfaceIP = "192.0.2.2"
	)

	cfg := mdns.Config{
		Interface:   discoverIfaceName,
		IP:          net.ParseIP(discoverIfaceIP),
		Port:        port,
		ServiceName: serviceName,
		ServiceType: "_snapd._https",
	}

	addresses, stop, err := mdns.MulticastDiscovery(ctx, cfg)
	c.Assert(err, check.IsNil)

	// calling stop should always close the channel. ensure this by draining the
	// channel until empty and closed.
	defer func() {
		for {
			select {
			case <-ctx.Done():
				c.Fatalf("context cancelled before channel could be drained")
			case _, ok := <-addresses:
				if !ok {
					return
				}
			}
		}
	}()

	defer stop()

	peers := make([]mdns.Config, 0, 4)
	for i := 0; i < 4; i++ {
		ip := net.ParseIP(fmt.Sprintf("192.0.2.%d", 100+i))
		c.Assert(ip, check.NotNil)
		ip = ip.To4()
		c.Assert(ip, check.NotNil)

		peers = append(peers, mdns.Config{
			Interface:   discoverIfaceName,
			IP:          ip,
			Port:        port,
			ServiceName: fmt.Sprintf("snapd-peer-%d", i+1),
			ServiceType: "_snapd._https",
		})
	}

	stop = startPeers(ctx, c, peers)
	defer stop()

	expected := make(map[string]bool, len(peers))
	for _, peer := range peers {
		expected[fmt.Sprintf("%s:%d", peer.IP.String(), peer.Port)] = true
	}

	// we also expect to discover ourself
	expected[fmt.Sprintf("%s:%d", discoverIfaceIP, port)] = true

	found := make(map[string]bool)
	for len(found) < len(expected) {
		select {
		case addr := <-addresses:
			if expected[addr] {
				found[addr] = true
			} else {
				c.Fatalf("unexpected multicast discovery: %s", addr)
			}
		case <-ctx.Done():
			c.Fatalf("timeout waiting for multicast discovery; want %v, have %v", keys(expected), keys(found))
		}
	}
}

func startPeers(ctx context.Context, c *check.C, peers []mdns.Config) func() {
	ctx, cancel := context.WithCancel(ctx)

	stops := make([]func(), 0, len(peers))
	addrs := make([]string, 0, len(peers))
	ifaces := make([]string, 0, len(peers))

	for _, peer := range peers {
		addrCIDR := fmt.Sprintf("%v/24", peer.IP)
		cmd := exec.Command("ip", "addr", "add", addrCIDR, "dev", peer.Interface)
		if output, err := cmd.CombinedOutput(); err != nil {
			cancel()
			c.Fatalf("cannot add address %s: %v (%s)", addrCIDR, err, strings.TrimSpace(string(output)))
		}

		ch, stop, err := mdns.MulticastDiscovery(ctx, peer)
		if err != nil {
			cancel()
			c.Fatalf("cannot start peer %s discovery: %v", peer.ServiceName, err)
		}

		stops = append(stops, stop)
		addrs = append(addrs, addrCIDR)
		ifaces = append(ifaces, peer.Interface)

		// just drain the channel here, the main peer is the one we're testing
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case _, ok := <-ch:
					if !ok {
						return
					}
				}
			}
		}()
	}

	return func() {
		cancel()

		for _, stop := range stops {
			stop()
		}

		for i, addr := range addrs {
			exec.Command("ip", "addr", "del", addr, "dev", ifaces[i]).Run()
		}
	}
}

// TODO:GOVERSION: remove this once we're on go 1.23
func keys[K comparable, V any](m map[K]V) []K {
	slice := make([]K, 0, len(m))
	for k := range m {
		slice = append(slice, k)
	}
	return slice
}
