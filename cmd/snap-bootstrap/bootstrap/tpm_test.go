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
package bootstrap_test

import (
	"os"
	"path/filepath"

	"github.com/chrisccoulson/ubuntu-core-fde-utils"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

type bootstrapTPMSuite struct {
	testutil.BaseTest

	dir string
}

var _ = Suite(&bootstrapTPMSuite{})

func (s *bootstrapTPMSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()
	dirs.SetRootDir(s.dir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *bootstrapTPMSuite) TestProvision(c *C) {
	n := 0
	restore := bootstrap.MockProvisionTPM(func(tpm *fdeutil.TPMConnection, mode fdeutil.ProvisionMode,
		newLockoutAuth []byte, auths *fdeutil.ProvisionAuths) error {
		c.Assert(mode, Equals, fdeutil.ProvisionModeFull)
		c.Assert(auths, IsNil)
		n++
		return nil
	})
	defer restore()

	t := bootstrap.TPMSupport{}
	err := t.Provision()
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
}

func (s *bootstrapTPMSuite) TestSeal(c *C) {
	n := 0
	myKey := []byte("528491")
	myKeyDest := "keyFilename"
	myPrivDest := "privateFilename"

	// dummy OS component files
	shimFile := filepath.Join(s.dir, "shim")
	f, err := os.Create(shimFile)
	f.Close()
	grubFile := filepath.Join(s.dir, "grub")
	f, err = os.Create(grubFile)
	f.Close()
	kernelFile := filepath.Join(s.dir, "kernel")
	f, err = os.Create(kernelFile)
	f.Close()

	t := bootstrap.TPMSupport{}
	t.SetShimFiles(shimFile)
	t.SetBootloaderFiles(grubFile)
	t.SetKernelFiles(kernelFile)

	restore := bootstrap.MockSealKeyToTPM(func(tpm *fdeutil.TPMConnection, keyDest, privDest string,
		create *fdeutil.CreationParams, policy *fdeutil.PolicyParams, key []byte) error {
		c.Assert(key, DeepEquals, myKey)
		c.Assert(keyDest, Equals, myKeyDest)
		c.Assert(privDest, Equals, myPrivDest)
		c.Assert(*create, DeepEquals, fdeutil.CreationParams{
			PolicyRevocationHandle: 0x01800001,
			PinHandle:              0x01800000,
			OwnerAuth:              nil,
		})
		c.Assert(policy.LoadPaths[0].LoadType, Equals, fdeutil.FirmwareLoad)
		c.Assert(policy.LoadPaths[0].Image, Equals, fdeutil.FileOSComponent(shimFile))
		c.Assert(policy.LoadPaths[0].Next, HasLen, 1)
		next := policy.LoadPaths[0].Next
		c.Assert(next[0].LoadType, Equals, fdeutil.DirectLoadWithShimVerify)
		c.Assert(next[0].Image, Equals, fdeutil.FileOSComponent(grubFile))
		c.Assert(next[0].Next, HasLen, 1)
		next = next[0].Next
		c.Assert(next[0].LoadType, Equals, fdeutil.DirectLoadWithShimVerify)
		c.Assert(next[0].Image, Equals, fdeutil.FileOSComponent(kernelFile))
		c.Assert(next[0].Next, HasLen, 0)
		n++
		return nil
	})
	defer restore()

	err = t.Seal(myKey, myKeyDest, myPrivDest)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
}

func (s *bootstrapTPMSuite) TestSetFiles(c *C) {
	t := &bootstrap.TPMSupport{}

	p1 := filepath.Join(s.dir, "f1")
	f, err := os.Create(p1)
	f.Close()

	// set shim files
	err = t.SetShimFiles("foo")
	c.Assert(err, ErrorMatches, "file foo does not exist")
	err = t.SetShimFiles(p1, "bar")
	c.Assert(err, ErrorMatches, "file bar does not exist")
	err = t.SetShimFiles(p1)
	c.Assert(err, IsNil)

	// set bootloader
	err = t.SetBootloaderFiles("foo")
	c.Assert(err, ErrorMatches, "file foo does not exist")
	err = t.SetBootloaderFiles(p1, "bar")
	c.Assert(err, ErrorMatches, "file bar does not exist")
	err = t.SetBootloaderFiles(p1)
	c.Assert(err, IsNil)

	// set kernel files
	err = t.SetKernelFiles("foo")
	c.Assert(err, ErrorMatches, "file foo does not exist")
	err = t.SetKernelFiles(p1, "bar")
	c.Assert(err, ErrorMatches, "file bar does not exist")
	err = t.SetKernelFiles(p1)
	c.Assert(err, IsNil)
}
