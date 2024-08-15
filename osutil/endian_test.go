// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package osutil_test

import (
	"encoding/binary"
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type endianTestSuite struct{}

var _ = Suite(&endianTestSuite{})

// back in 14.04/16.04 32bit powerpc was supported via gccgo
var knownGccGoArch = map[string]bool{
	"ppc": true,
}

// copied from:
// https://github.com/golang/go/blob/release-branch.go1.20/src/go/build/syslist.go#L53
// alternatively this could be done via "go tool dist list" but seems not
// worth the extra parsing
var knownArch = map[string]bool{
	"386":         true,
	"amd64":       true,
	"amd64p32":    true,
	"arm":         true,
	"armbe":       true,
	"arm64":       true,
	"arm64be":     true,
	"loong64":     true,
	"mips":        true,
	"mipsle":      true,
	"mips64":      true,
	"mips64le":    true,
	"mips64p32":   true,
	"mips64p32le": true,
	"ppc":         true,
	"ppc64":       true,
	"ppc64le":     true,
	"riscv":       true,
	"riscv64":     true,
	"s390":        true,
	"s390x":       true,
	"sparc":       true,
	"sparc64":     true,
	"wasm":        true,
}

func knownGoArch(arch string) error {
	// this knownGccGoArch map can be removed after 16.04 goes EOL
	// in 2026
	if knownGccGoArch[arch] {
		return nil
	}

	if knownArch[arch] {
		return nil
	}

	return fmt.Errorf("cannot find %s in supported go arches", arch)
}

func (s *endianTestSuite) TestKnownGoArch(c *C) {
	c.Check(knownGoArch("not-supported-arch"), ErrorMatches, "cannot find not-supported-arch in supported go arches")
}

func (s *endianTestSuite) TestEndian(c *C) {
	for _, t := range []struct {
		arch   string
		endian binary.ByteOrder
	}{
		{"ppc", binary.BigEndian},
		{"ppc64", binary.BigEndian},
		{"s390x", binary.BigEndian},
		{"386", binary.LittleEndian},
		{"amd64", binary.LittleEndian},
		{"arm", binary.LittleEndian},
		{"arm64", binary.LittleEndian},
		{"ppc64le", binary.LittleEndian},
		{"riscv64", binary.LittleEndian},
	} {
		restore := osutil.MockRuntimeGOARCH(t.arch)
		defer restore()

		c.Check(osutil.Endian(), Equals, t.endian)
		c.Check(knownGoArch(t.arch), IsNil)
	}
}

func (s *endianTestSuite) TestEndianErrors(c *C) {
	restore := osutil.MockRuntimeGOARCH("unknown-arch")
	defer restore()

	c.Check(func() { osutil.Endian() }, Panics, "unknown architecture unknown-arch")
}
