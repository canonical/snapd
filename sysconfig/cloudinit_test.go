// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package sysconfig_test

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type sysconfigSuite struct {
	testutil.BaseTest

	tmpdir string
}

var _ = Suite(&sysconfigSuite{})

func (s *sysconfigSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *sysconfigSuite) TestCloudInitDisablesByDefault(c *C) {
	err := sysconfig.ConfigureRunSystem(&sysconfig.Options{
		TargetRootDir: boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled/")
	c.Check(ubuntuDataCloudDisabled, testutil.FilePresent)
}

func (s *sysconfigSuite) TestCloudInitInstalls(c *C) {
	cloudCfgSrcDir := c.MkDir()
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err := ioutil.WriteFile(filepath.Join(cloudCfgSrcDir, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	err := sysconfig.ConfigureRunSystem(&sysconfig.Options{
		CloudInitSrcDir: cloudCfgSrcDir,
		TargetRootDir:   boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	// and did copy the cloud-init files
	ubuntuDataCloudCfg := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud.cfg.d/")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "foo.cfg"), testutil.FileEquals, "foo.cfg config")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "bar.cfg"), testutil.FileEquals, "bar.cfg config")
}
