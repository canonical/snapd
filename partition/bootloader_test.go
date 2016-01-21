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

package partition

import (
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// partition specific testsuite
type PartitionTestSuite struct {
}

var _ = Suite(&PartitionTestSuite{})

type mockBootloader struct {
	bootVars map[string]string
}

func newMockBootloader() *mockBootloader {
	return &mockBootloader{
		bootVars: make(map[string]string),
	}
}
func (b *mockBootloader) Dir() string {
	return "/foo"
}
func (b *mockBootloader) GetBootVar(name string) (string, error) {
	return b.bootVars[name], nil
}
func (b *mockBootloader) SetBootVar(name, value string) error {
	b.bootVars[name] = value
	return nil
}

func (s *PartitionTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll((&grub{}).Dir(), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll((&uboot{}).Dir(), 0755)
	c.Assert(err, IsNil)
}

func (s *PartitionTestSuite) TestMarkBootSuccessfulAllSnap(c *C) {
	b := newMockBootloader()
	b.bootVars["snappy_os"] = "os1"
	b.bootVars["snappy_kernel"] = "k1"
	err := MarkBootSuccessful(b)
	c.Assert(err, IsNil)
	c.Assert(b.bootVars, DeepEquals, map[string]string{
		"snappy_mode":        "regular",
		"snappy_trial_boot":  "0",
		"snappy_kernel":      "k1",
		"snappy_good_kernel": "k1",
		"snappy_os":          "os1",
		"snappy_good_os":     "os1",
	})
}
