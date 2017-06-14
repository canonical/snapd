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

	// forked from "golang.org/x/net/bpf"
	// until https://github.com/golang/go/issues/20556
	"github.com/mvo5/net/bpf"

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
		{"ioctl - TIOCSTI", "ioctl;native;0,99", main.SeccompRetKill},
		{"ioctl - !TIOCSTI", "ioctl;native;0,TIOCSTI", main.SeccompRetKill},

		// test_bad_seccomp_filter_args_clone
		{"setns - CLONE_NEWNET", "setns;native;0,99", main.SeccompRetKill},
		{"setns - CLONE_NEWNET", "setns;native;0,CLONE_NEWNET", main.SeccompRetAllow},
		// test_bad_seccomp_filter_args_mknod
		{"mknod - |S_IFIFO", "mknod;native;0,S_IFIFO", main.SeccompRetAllow},
		{"mknod - |S_IFIFO", "mknod;native;0,99", main.SeccompRetKill},
		// test_bad_seccomp_filter_args_prctl
		{"prctl PR_CAP_AMBIENT_RAISE", "prctl;native;PR_CAP_AMBIENT_RAISE", main.SeccompRetAllow},
		{"prctl PR_CAP_AMBIENT_RAISE", "prctl;native;99", main.SeccompRetKill},
		// test_bad_seccomp_filter_args_prio
		{"setpriority PRIO_PROCESS 0 >=0", "setpriority;native;PRIO_PROCESS,0,19", main.SeccompRetAllow},
		{"setpriority PRIO_PROCESS 0 >=0", "setpriority;native;99", main.SeccompRetKill},
		// test_bad_seccomp_filter_args_quotactl
		{"quotactl Q_GETQUOTA", "quotactl;native;Q_GETQUOTA", main.SeccompRetAllow},
		{"quotactl Q_GETQUOTA", "quotactl;native;99", main.SeccompRetKill},
		// test_bad_seccomp_filter_args_socket
		{"socket AF_UNIX", "socket;native;AF_UNIX", main.SeccompRetAllow},
		{"socket AF_UNIX", "socket;native;99", main.SeccompRetKill},
		{"socket - SOCK_STREAM", "socket;native;0,SOCK_STREAM", main.SeccompRetAllow},
		{"socket - SOCK_STREAM", "socket;native;0,99", main.SeccompRetKill},
		// test_bad_seccomp_filter_args_termios
		{"ioctl - TIOCSTI", "ioctl;native;0,TIOCSTI", main.SeccompRetAllow},
		{"ioctl - TIOCSTI", "ioctl;native;0,99", main.SeccompRetKill},
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
		c.Check(out, Equals, t.expected, Commentf("unexpected result for %q (input %q), got %v expected %v", t.seccompWhitelist, t.bpfInput, out, t.expected))
	}

}

func (s *snapSeccompSuite) TestCompileBadInput(c *C) {
	for _, t := range []struct {
		inp    string
		errMsg string
	}{
		// test_bad_seccomp_filter_args_clone (various typos in input)
		{"setns - CLONE_NEWNE", `cannot parse line: cannot parse token "CLONE_NEWNE" \(line "setns - CLONE_NEWNE"\)`},
		{"setns - CLONE_NEWNETT", `cannot parse line: cannot parse token "CLONE_NEWNETT" \(line "setns - CLONE_NEWNETT"\)`},
		{"setns - CL0NE_NEWNET", `cannot parse line: cannot parse token "CL0NE_NEWNET" \(line "setns - CL0NE_NEWNET"\)`},
		// test_bad_seccomp_filter_args_mknod (various typos in input)
		{"mknod - |S_IFIF", `cannot parse line: cannot parse token "S_IFIF" \(line "mknod - |S_IFIF"\)`},
		{"mknod - |S_IFIFOO", `cannot parse line: cannot parse token "S_IFIFOO" \(line "mknod - |S_IFIFOO"\)`},
		{"mknod - |S_!FIFO", `cannot parse line: cannot parse token "S_IFIFO" \(line "mknod - |S_!FIFO"\)`},
		// test_bad_seccomp_filter_args_null
		{"socket S\x00CK_STREAM", `cannot parse line: cannot parse token .*`},
		{"socket SOCK_STREAM\x00bad stuff", `cannot parse line: cannot parse token .*`},

		// test_bad_seccomp_filter_args
		{"mbind - - - - - - 7", `cannot parse line: too many tokens \(6\) in line.*`},
		{"mbind 1 2 3 4 5 6 7", `cannot parse line: too many tokens \(6\) in line.*`},
		// test_bad_seccomp_filter_args_prctl
		{"prctl PR_GET_SECCOM", `cannot parse line: cannot parse token "PR_GET_SECCOM" .*`},
		{"prctl PR_GET_SECCOMPP", `cannot parse line: cannot parse token "PR_GET_SECCOMPP" .*`},
		{"prctl PR_GET_SECC0MP", `cannot parse line: cannot parse token "PR_GET_SECC0MP" .*`},
		{"prctl PR_CAP_AMBIENT_RAIS", `cannot parse line: cannot parse token "PR_CAP_AMBIENT_RAIS" .*`},
		{"prctl PR_CAP_AMBIENT_RAISEE", `cannot parse line: cannot parse token "PR_CAP_AMBIENT_RAISEE" .*`},
		// test_bad_seccomp_filter_args_prio
		{"setpriority PRIO_PROCES 0 >=0", `cannot parse line: cannot parse token "PRIO_PROCES" .*`},
		{"setpriority PRIO_PROCESSS 0 >=0", `cannot parse line: cannot parse token "PRIO_PROCESSS" .*`},
		{"setpriority PRIO_PR0CESS 0 >=0", `cannot parse line: cannot parse token "PRIO_PR0CESS" .*`},
		// test_bad_seccomp_filter_args_quotactl
		{"quotactl Q_GETQUOT", `cannot parse line: cannot parse token "Q_GETQUOT" .*`},
		{"quotactl Q_GETQUOTAA", `cannot parse line: cannot parse token "Q_GETQUOTAA" .*`},
		{"quotactl Q_GETQU0TA", `cannot parse line: cannot parse token "Q_GETQU0TA" .*`},
		// test_bad_seccomp_filter_args_socket
		{"socket AF_UNI", `cannot parse line: cannot parse token "AF_UNI" .*`},
		{"socket AF_UNIXX", `cannot parse line: cannot parse token "AF_UNIXX" .*`},
		{"socket AF_UN!X", `cannot parse line: cannot parse token "AF_UN!X" .*`},
		{"socket - SOCK_STREA", `cannot parse line: cannot parse token "SOCK_STREA" .*`},
		{"socket - SOCK_STREAMM", `cannot parse line: cannot parse token "SOCK_STREAMM" .*`},
		// test_bad_seccomp_filter_args_termios
		{"ioctl - TIOCST", `cannot parse line: cannot parse token "TIOCST" .*`},
		{"ioctl - TIOCSTII", `cannot parse line: cannot parse token "TIOCSTII" .*`},
		{"ioctl - TIOCST1", `cannot parse line: cannot parse token "TIOCST1" .*`},
	} {
		outPath := filepath.Join(c.MkDir(), "bpf")
		err := main.Compile([]byte(t.inp), outPath)
		c.Check(err, ErrorMatches, t.errMsg, Commentf("%q errors in unexpected ways, got: %q expected %q", t.inp, err, t.errMsg))
	}
}
