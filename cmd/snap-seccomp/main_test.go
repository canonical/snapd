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
	"io/ioutil"
	"os"
	"path/filepath"
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

func cmpBpf(c *C, p string, expected []byte) {
	content, err := ioutil.ReadFile(p)
	c.Assert(err, IsNil)

	c.Check(content, DeepEquals, expected)
}

func (s *snapSeccompSuite) TestCompileTrivial(c *C) {
	for _, syscall := range []string{"read", "write", "execve"} {

		content := []byte(syscall)
		outPath := filepath.Join(c.MkDir(), "bpf")

		err := main.Compile(content, outPath)
		c.Assert(err, IsNil)

		r, err := os.Open(outPath)
		c.Assert(err, IsNil)
		defer r.Close()

		sc, err := seccomp.GetSyscallFromName(syscall)
		c.Assert(err, IsNil)

		var rawOp bpf.RawInstruction
		found := false
		for {
			err = binary.Read(r, binary.LittleEndian, &rawOp)
			if err == io.EOF {
				break
			}
			op := rawOp.Disassemble()
			if jmpOp, ok := op.(bpf.JumpIf); ok {
				if jmpOp.Val == uint32(sc) {
					found = true
				}
			}
		}
		c.Check(found, Equals, true)
	}
}

type seccompData struct {
	nr                 uint32
	arch               uint32
	instructionPointer uint64
	args               [6]uint64
}

const (
	// from #include<linux/seccomp.h>
	seccompRetKill  = 0
	seccompRetAllow = 0x7fff0000
)

func (s *snapSeccompSuite) TestCompileSimulate(c *C) {
	content := []byte("read\nwrite\n")
	outPath := filepath.Join(c.MkDir(), "bpf")

	err := main.Compile(content, outPath)
	c.Assert(err, IsNil)

	r, err := os.Open(outPath)
	c.Assert(err, IsNil)
	defer r.Close()

	var ops []bpf.Instruction
	for {
		var rawOp bpf.RawInstruction
		err = binary.Read(r, binary.LittleEndian, &rawOp)
		if err == io.EOF {
			break
		}
		ops = append(ops, rawOp.Disassemble())
	}

	scRead, err := seccomp.GetSyscallFromName("read")
	c.Assert(err, IsNil)

	vm, err := bpf.NewVM(ops)
	c.Assert(err, IsNil)

	sdGood := seccompData{
		nr:                 uint32(scRead),
		arch:               3221225534, // x64 - FIXME: get via libseccomp
		instructionPointer: 99,         // random
		args:               [6]uint64{1, 2, 3, 4, 5, 6},
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
		nr:                 uint32(scExecve),
		arch:               3221225534, // x64 - FIXME: get via libseccomp
		instructionPointer: 99,         // random
		args:               [6]uint64{1, 2, 3, 4, 5, 6},
	}
	buf = bytes.NewBuffer(nil)
	binary.Write(buf, binary.BigEndian, &sdBad)

	out, err = vm.Run(buf.Bytes())
	c.Assert(err, IsNil)
	c.Check(out, Equals, seccompRetKill)

}
