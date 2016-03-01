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

package helpers

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type HTestSuite struct{}

var _ = Suite(&HTestSuite{})

func (ts *HTestSuite) TestMakeMapFromEnvList(c *C) {
	envList := []string{
		"PATH=/usr/bin:/bin",
		"DBUS_SESSION_BUS_ADDRESS=unix:abstract=something1234",
	}
	envMap := MakeMapFromEnvList(envList)
	c.Assert(envMap, DeepEquals, map[string]string{
		"PATH": "/usr/bin:/bin",
		"DBUS_SESSION_BUS_ADDRESS": "unix:abstract=something1234",
	})
}

func (ts *HTestSuite) TestMakeMapFromEnvListInvalidInput(c *C) {
	envList := []string{
		"nonsesne",
	}
	envMap := MakeMapFromEnvList(envList)
	c.Assert(envMap, DeepEquals, map[string]string(nil))
}

func (ts *HTestSuite) TestMakeRandomString(c *C) {
	// for our tests
	rand.Seed(1)

	s1 := MakeRandomString(10)
	c.Assert(s1, Equals, "pw7MpXh0JB")

	s2 := MakeRandomString(5)
	c.Assert(s2, Equals, "4PQyl")
}

func skipOnMissingDevKmsg(c *C) {
	_, err := os.Stat("/dev/kmsg")
	if err != nil {
		c.Skip("Can not stat /dev/kmsg")
	}
}

func (ts *HTestSuite) TestGetattr(c *C) {
	T := struct {
		S string
		I int
	}{
		S: "foo",
		I: 42,
	}
	// works on values
	c.Assert(Getattr(T, "S").(string), Equals, "foo")
	c.Assert(Getattr(T, "I").(int), Equals, 42)
	// works for pointers too
	c.Assert(Getattr(&T, "S").(string), Equals, "foo")
	c.Assert(Getattr(&T, "I").(int), Equals, 42)
}

func makeTestFiles(c *C, srcDir, destDir string) {
	// a new file
	err := ioutil.WriteFile(filepath.Join(srcDir, "new"), []byte(nil), 0644)
	c.Assert(err, IsNil)

	// a existing file that needs update
	err = ioutil.WriteFile(filepath.Join(destDir, "existing-update"), []byte("old-content"), 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(srcDir, "existing-update"), []byte("some-new-content"), 0644)
	c.Assert(err, IsNil)

	// existing file that needs no update
	err = ioutil.WriteFile(filepath.Join(srcDir, "existing-unchanged"), []byte(nil), 0644)
	c.Assert(err, IsNil)
	err = exec.Command("cp", "-a", filepath.Join(srcDir, "existing-unchanged"), filepath.Join(destDir, "existing-unchanged")).Run()
	c.Assert(err, IsNil)

	// a file that needs removal
	err = ioutil.WriteFile(filepath.Join(destDir, "to-be-deleted"), []byte(nil), 0644)
	c.Assert(err, IsNil)
}

func compareDirs(c *C, srcDir, destDir string) {
	d1, err := exec.Command("ls", "-al", srcDir).CombinedOutput()
	c.Assert(err, IsNil)
	d2, err := exec.Command("ls", "-al", destDir).CombinedOutput()
	c.Assert(err, IsNil)
	c.Assert(string(d1), Equals, string(d2))
	// ensure content got updated
	c1, err := exec.Command("sh", "-c", fmt.Sprintf("find %s -type f |xargs cat", srcDir)).CombinedOutput()
	c.Assert(err, IsNil)
	c2, err := exec.Command("sh", "-c", fmt.Sprintf("find %s -type f |xargs cat", destDir)).CombinedOutput()
	c.Assert(err, IsNil)
	c.Assert(string(c1), Equals, string(c2))
}
