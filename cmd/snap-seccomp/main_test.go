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
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/seccomp/libseccomp-golang"
	"golang.org/x/net/bpf"

	main "github.com/snapcore/snapd/cmd/snap-seccomp"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type snapSeccompSuite struct{}

var _ = Suite(&snapSeccompSuite{})

// from linux/seccomp.h: struct seccomp_data
type seccompData struct {
	// FIXME: "int" in linux/seccomp.h
	syscallNr          uint32
	arch               uint32
	instructionPointer uint64
	syscallArgs        [6]uint64
}

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
		// FIXME: native endian here?
		err = binary.Read(r, binary.LittleEndian, &rawOp)
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

func parseBpfInput(s string) (*seccompData, error) {
	// syscall;arch;arg1,arg2...
	l := strings.Split(s, ";")

	sc, err := seccomp.GetSyscallFromName(l[0])
	if err != nil {
		return nil, err
	}

	var arch uint32
	if len(l) < 2 || l[1] == "native" {
		arch = goArchToScmpArch(runtime.GOARCH)
	}

	return &seccompData{
		syscallNr: uint32(sc),
		arch:      arch,
	}, nil
}

func (s *snapSeccompSuite) TestCompile(c *C) {
	for _, t := range []struct {
		seccompWhitelist string
		bpfInput         string
		expected         int
	}{
		{"@unrestricted", "execve", main.SeccompRetAllow},

		{"read", "read", main.SeccompRetAllow},
		{"read\nwrite\nexecve\n", "write", main.SeccompRetAllow},

		{"read", "execve", main.SeccompRetKill},
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

		buf := bytes.NewBuffer(nil)
		binary.Write(buf, binary.BigEndian, bpfSeccompInput)

		out, err := vm.Run(buf.Bytes())
		c.Assert(err, IsNil)
		c.Check(out, Equals, t.expected)
	}

}
