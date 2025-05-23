// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package blkid_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/blkid"

	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type blkidSuite struct {
	testutil.BaseTest

	image string
}

var _ = Suite(&blkidSuite{})

func (s *blkidSuite) SetUpTest(c *C) {
	systemdRepart, err := exec.LookPath("systemd-repart")
	if err != nil {
		c.Skip("systemd-repart is not available")
	}

	s.BaseTest.SetUpTest(c)

	tmp := c.MkDir()
	image := filepath.Join(tmp, "image")

	cmd := exec.Command(systemdRepart, "--offline=yes", "--size=64M", "--empty=create", "--definitions=test-data/repart.d", image)
	err = cmd.Run()
	if err != nil {
		c.Skip("systemd-repart is not working")
	}

	s.image = image
}

func (s *blkidSuite) TestScanPartitionTable(c *C) {
	probe, err := blkid.NewProbeFromFilename(s.image)
	c.Assert(err, IsNil)
	defer probe.Close()

	probe.EnablePartitions(true)

	err = probe.DoSafeprobe()
	c.Assert(err, IsNil)

	pttype, err := probe.LookupValue("PTTYPE")
	c.Assert(err, IsNil)
	c.Check(pttype, Equals, "gpt")

	partlist, err := probe.GetPartitions()
	c.Assert(err, IsNil)

	partitions := 0
	for _, p := range partlist.GetPartitions() {
		partitions++
		label := p.GetName()
		c.Check(label, Equals, "thelabel")
		uuid := p.GetUUID()
		c.Check(uuid, Equals, "93fd7d8f-a662-4451-a03f-c065f2c2e1ab")
	}
	c.Check(partitions, Equals, 1)
}
