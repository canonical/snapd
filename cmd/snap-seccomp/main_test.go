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
	"io"
	"os"
	"path/filepath"
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

const (
	// from #include<linux/seccomp.h>
	seccompRetKill  = 0
	seccompRetAllow = 0x7fff0000
)

// from linux/seccomp.h: struct seccomp_data
type seccompData struct {
	// FIXME: "int" in linux/seccomp.h
	syscallNr          uint32
	arch               uint32
	instructionPointer uint64
	syscallArgs        [6]uint64
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
	if len(l) < 2 {
		// FIXME: use native arch
		// x64 - FIXME: get via libseccomp
		arch = 3221225534
	}

	return &seccompData{
		syscallNr: uint32(sc),
		arch:      arch,
	}, nil
}

func (s *snapSeccompSuite) TestCompile(c *C) {
	for _, t := range []struct {
		bpfProg  string
		bpfInput string
		expected int
	}{
		{"read", "read", seccompRetAllow},
		{"read", "execve", seccompRetKill},
	} {
		outPath := filepath.Join(c.MkDir(), "bpf")
		err := main.Compile([]byte(t.bpfProg), outPath)
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

func (s *snapSeccompSuite) TestCompileSimulate(c *C) {
	content := []byte("read\nwrite\n")
	outPath := filepath.Join(c.MkDir(), "bpf")

	err := main.Compile(content, outPath)
	c.Assert(err, IsNil)

	ops, err := decodeBpfFromFile(outPath)
	c.Assert(err, IsNil)

	scRead, err := seccomp.GetSyscallFromName("read")
	c.Assert(err, IsNil)

	vm, err := bpf.NewVM(ops)
	c.Assert(err, IsNil)

	sdGood := seccompData{
		syscallNr:          uint32(scRead),
		arch:               3221225534, // x64 - FIXME: get via libseccomp
		instructionPointer: 99,         // random
		syscallArgs:        [6]uint64{1, 2, 3, 4, 5, 6},
	}
	buf := bytes.NewBuffer(nil)
	binary.Write(buf, binary.BigEndian, &sdGood)

	out, err := vm.Run(buf.Bytes())
	c.Assert(err, IsNil)
	c.Check(out, Equals, seccompRetAllow)

	// simulate a bad run
	scExecve, err := seccomp.GetSyscallFromName("execve")
	c.Assert(err, IsNil)
	sdBad := seccompData{
		syscallNr:          uint32(scExecve),
		arch:               3221225534, // x64 - FIXME: get via libseccomp
		instructionPointer: 99,         // random
		syscallArgs:        [6]uint64{1, 2, 3, 4, 5, 6},
	}
	buf = bytes.NewBuffer(nil)
	binary.Write(buf, binary.BigEndian, &sdBad)

	out, err = vm.Run(buf.Bytes())
	c.Assert(err, IsNil)
	c.Check(out, Equals, seccompRetKill)

}
