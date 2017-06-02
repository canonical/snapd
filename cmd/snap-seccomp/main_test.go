// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main_test

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	. "gopkg.in/check.v1"

	"github.com/seccomp/libseccomp-golang"
	"golang.org/x/net/bpf"

	main "github.com/snapcore/snapd/cmd/snap-seccomp"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type snapSeccompSuite struct{}

var _ = Suite(&snapSeccompSuite{})

func goArchToScmpArch(goarch string) uint32 {
	switch goarch {
	case "386":
		return main.ScmpArchX86
	case "amd64":
		return main.ScmpArchX86_64
	case "arm":
		return main.ScmpArchARM
	case "arm64":
		return main.ScmpArchAARCH64
	case "ppc64le":
		return main.ScmpArchPPC64LE
	case "s390x":
		return main.ScmpArchS390X
	case "ppc":
		return main.ScmpArchPPC
	}
	panic(fmt.Sprintf("cannot map goarch %q to a seccomp arch", goarch))
}

func decodeBpfFromFile(p string) ([]bpf.Instruction, error) {
	var ops []bpf.Instruction
	var rawOp bpf.RawInstruction

	r, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for {
		err = binary.Read(r, nativeEndian(), &rawOp)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		ops = append(ops, rawOp.Disassemble())
	}

	return ops, nil
}

func parseBpfInput(s string) (*main.SeccompData /* *seccompData */, error) {
	// syscall;arch;arg1,arg2...
	l := strings.Split(s, ";")

	sc, err := seccomp.GetSyscallFromName(l[0])
	if err != nil {
		return nil, err
	}

	var arch uint32
	if len(l) < 2 || l[1] == "native" {
		arch = goArchToScmpArch(runtime.GOARCH)
	} else {
		arch = goArchToScmpArch(l[1])
	}

	var syscallArgs [6]uint64
	if len(l) > 2 {
		args := strings.Split(l[2], ",")
		for i := range args {
			if nr, err := strconv.ParseUint(args[i], 10, 64); err == nil {
				syscallArgs[i] = nr
			} else {
				syscallArgs[i] = main.SeccompResolver[args[i]]
			}
		}
	}
	sd := &main.SeccompData{}
	sd.SetArch(arch)
	sd.SetNr(sc)
	sd.SetArgs(syscallArgs)
	return sd, nil
}

// Endianness detection.
func nativeEndian() binary.ByteOrder {
	// Credit matt kane, taken from his gosndfile project.
	// https://groups.google.com/forum/#!msg/golang-nuts/3GEzwKfRRQw/D1bMbFP-ClAJ
	// https://github.com/mkb218/gosndfile
	var i int32 = 0x01020304
	u := unsafe.Pointer(&i)
	pb := (*byte)(u)
	b := *pb
	isLittleEndian := b == 0x04

	if isLittleEndian {
		return binary.LittleEndian
	} else {
		return binary.BigEndian
	}
}

func (s *snapSeccompSuite) TestCompile(c *C) {
	// FIXME: this will only work once the following issue is fixed:
	// https://github.com/golang/go/issues/20556
	bpf.VmEndianness = nativeEndian()

	for _, t := range []struct {
		seccompWhitelist string
		bpfInput         string
		expected         int
	}{
		// special
		{"@unrestricted", "execve", main.SeccompRetAllow},
		{"@complain", "execve", main.SeccompRetAllow},

		// trivial alllow
		{"read", "read", main.SeccompRetAllow},
		{"read\nwrite\nexecve\n", "write", main.SeccompRetAllow},

		// trivial denial
		{"read", "execve", main.SeccompRetKill},

		// argument filtering
		{"read >=2", "read;native;2", main.SeccompRetAllow},
		{"read >=2", "read;native;1", main.SeccompRetKill},

		{"read <=2", "read;native;2", main.SeccompRetAllow},
		{"read <=2", "read;native;3", main.SeccompRetKill},

		{"read !2", "read;native;3", main.SeccompRetAllow},
		{"read !2", "read;native;2", main.SeccompRetKill},

		{"read >2", "read;native;2", main.SeccompRetKill},
		{"read >2", "read;native;3", main.SeccompRetAllow},

		{"read <2", "read;native;2", main.SeccompRetKill},
		{"read <2", "read;native;1", main.SeccompRetAllow},

		// FIXME: test maskedEqual better
		{"read |1", "read;native;1", main.SeccompRetAllow},
		{"read |1", "read;native;2", main.SeccompRetKill},

		{"read 2", "read;native;3", main.SeccompRetKill},
		{"read 2", "read;native;2", main.SeccompRetAllow},

		// with arg1 and name resolving
		{"ioctl - TIOCSTI", "ioctl;native;0,TIOCSTI", main.SeccompRetAllow},
		{"ioctl - !TIOCSTI", "ioctl;native;0,TIOCSTI", main.SeccompRetKill},
	} {
		outPath := filepath.Join(c.MkDir(), "bpf")
		err := main.Compile([]byte(t.seccompWhitelist), outPath)
		c.Assert(err, IsNil)

		ops, err := decodeBpfFromFile(outPath)
		c.Assert(err, IsNil)

		vm, err := bpf.NewVM(ops)
		c.Assert(err, IsNil)

		bpfSeccompInput, err := parseBpfInput(t.bpfInput)
		c.Assert(err, IsNil)

		buf2 := (*[64]byte)(unsafe.Pointer(bpfSeccompInput))

		out, err := vm.Run(buf2[:])
		c.Assert(err, IsNil)
		c.Check(out, Equals, t.expected, Commentf("unexpected result for %q, got %v expected %v", t.seccompWhitelist, out, t.expected))
	}

}
