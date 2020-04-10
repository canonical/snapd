// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

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

package secboot_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/chrisccoulson/go-tpm2"
	sb "github.com/snapcore/secboot"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

func TestSecboot(t *testing.T) { TestingT(t) }

type secbootTPMSuite struct {
	testutil.BaseTest

	dir string
}

var _ = Suite(&secbootTPMSuite{})

func (s *secbootTPMSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()
	dirs.SetRootDir(s.dir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *secbootTPMSuite) TestProvision(c *C) {
	n := 0
	restore := secboot.MockSbProvisionTPM(func(tpm *sb.TPMConnection, mode sb.ProvisionMode, newLockoutAuth []byte) error {
		c.Assert(mode, Equals, sb.ProvisionModeFull)
		n++
		return nil
	})
	defer restore()

	t := secboot.TPMSupport{}
	err := t.Provision()
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
}

func (s *secbootTPMSuite) TestSeal(c *C) {
	n := 0
	myKey := []byte("528491")
	myKeyPath := "keyFilename"
	myPolicyUpdatePath := "policyUpdateFilename"

	// dummy OS component files
	shim1 := filepath.Join(s.dir, "shim1")
	shim2 := filepath.Join(s.dir, "shim2")
	grub1 := filepath.Join(s.dir, "grub1")
	grub2 := filepath.Join(s.dir, "grub2")
	kernel1 := filepath.Join(s.dir, "kernel1")
	kernel2 := filepath.Join(s.dir, "kernel2")

	shimFiles := []string{shim1, shim2}
	grubFiles := []string{grub1, grub2}
	kernelFiles := []string{kernel1, kernel2}

	files := append(shimFiles, grubFiles...)
	files = append(files, kernelFiles...)

	for _, p := range files {
		c.Assert(ioutil.WriteFile(p, nil, 0644), IsNil)
	}

	t := secboot.TPMSupport{}
	c.Assert(t.SetShimFiles(shim1, shim2), IsNil)
	c.Assert(t.SetBootloaderFiles(grub1, grub2), IsNil)
	c.Assert(t.SetKernelFiles(kernel1, kernel2), IsNil)

	cmdlines := []string{"cmdline1", "cmdline2"}

	t.SetKernelCmdlines(cmdlines)

	loadSequences := []*sb.EFIImageLoadEvent{}

	for _, shim := range shimFiles {
		s := &sb.EFIImageLoadEvent{
			Source: sb.Firmware,
			Image:  sb.FileEFIImage(shim),
		}
		for _, grub := range grubFiles {
			g := &sb.EFIImageLoadEvent{
				Source: sb.Shim,
				Image:  sb.FileEFIImage(grub),
			}
			for _, kernel := range kernelFiles {
				k := &sb.EFIImageLoadEvent{
					Source: sb.Shim,
					Image:  sb.FileEFIImage(kernel),
				}
				g.Next = append(g.Next, k)
			}
			s.Next = append(s.Next, g)
		}
		loadSequences = append(loadSequences, s)
	}

	sbRestore := secboot.MockSbAddEFISecureBootPolicyProfile(func(profile *sb.PCRProtectionProfile,
		params *sb.EFISecureBootPolicyProfileParams) error {
		c.Assert(*params, DeepEquals, sb.EFISecureBootPolicyProfileParams{
			PCRAlgorithm:  tpm2.HashAlgorithmSHA256,
			LoadSequences: loadSequences,
		})
		return nil
	})
	defer sbRestore()

	stubRestore := secboot.MockSbAddSystemdEFIStubProfile(func(profile *sb.PCRProtectionProfile, params *sb.SystemdEFIStubProfileParams) error {
		c.Assert(*params, DeepEquals, sb.SystemdEFIStubProfileParams{
			PCRAlgorithm:   tpm2.HashAlgorithmSHA256,
			PCRIndex:       12,
			KernelCmdlines: cmdlines,
		})
		return nil
	})
	defer stubRestore()

	sealRestore := secboot.MockSbSealKeyToTPM(func(tpm *sb.TPMConnection, key []byte, keyPath, policyUpdatePath string, params *sb.KeyCreationParams) error {
		c.Assert(key, DeepEquals, myKey)
		c.Assert(keyPath, Equals, myKeyPath)
		c.Assert(policyUpdatePath, Equals, policyUpdatePath)
		c.Assert(params.PINHandle, Equals, tpm2.Handle(0x01800000))
		n++
		return nil
	})
	defer sealRestore()

	err := t.Seal(myKey, myKeyPath, myPolicyUpdatePath)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
}

func (s *secbootTPMSuite) TestSetFiles(c *C) {
	t := &secboot.TPMSupport{}

	p1 := filepath.Join(s.dir, "f1")
	c.Assert(ioutil.WriteFile(p1, nil, 0644), IsNil)

	// set shim files
	err := t.SetShimFiles("foo")
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
