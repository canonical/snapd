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

func simulateBpf(c *C, seccompWhitelist, bpfInput string, expected int) {
	outPath := filepath.Join(c.MkDir(), "bpf")
	err := main.Compile([]byte(seccompWhitelist), outPath)
	c.Assert(err, IsNil)

	ops, err := decodeBpfFromFile(outPath)
	c.Assert(err, IsNil)

	vm, err := bpf.NewVM(ops)
	c.Assert(err, IsNil)

	bpfSeccompInput, err := parseBpfInput(bpfInput)
	c.Assert(err, IsNil)

	buf2 := (*[64]byte)(unsafe.Pointer(bpfSeccompInput))

	out, err := vm.Run(buf2[:])
	c.Assert(err, IsNil)
	c.Check(out, Equals, expected, Commentf("unexpected result for %q (input %q), got %v expected %v", seccompWhitelist, bpfInput, out, expected))
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
		// test_restrictions_working_args_clone
		{"setns - CLONE_NEWIPC", "setns;native;0,CLONE_NEWIPC", main.SeccompRetAllow},
		{"setns - CLONE_NEWNET", "setns;native;0,CLONE_NEWNET", main.SeccompRetAllow},
		{"setns - CLONE_NEWNS", "setns;native;0,CLONE_NEWNS", main.SeccompRetAllow},
		{"setns - CLONE_NEWPID", "setns;native;0,CLONE_NEWPID", main.SeccompRetAllow},
		{"setns - CLONE_NEWUSER", "setns;native;0,CLONE_NEWUSER", main.SeccompRetAllow},
		{"setns - CLONE_NEWUTS", "setns;native;0,CLONE_NEWUTS", main.SeccompRetAllow},
		{"setns - CLONE_NEWIPC", "setns;native;0,99", main.SeccompRetKill},
		{"setns - CLONE_NEWNET", "setns;native;0,99", main.SeccompRetKill},
		{"setns - CLONE_NEWNS", "setns;native;0,99", main.SeccompRetKill},
		{"setns - CLONE_NEWPID", "setns;native;0,99", main.SeccompRetKill},
		{"setns - CLONE_NEWUSER", "setns;native;0,99", main.SeccompRetKill},
		{"setns - CLONE_NEWUTS", "setns;native;0,99", main.SeccompRetKill},
		// test_restrictions_working_args_mkdnor
		{"mknod - S_IFREG", "mknod;native;0,S_IFREG", main.SeccompRetAllow},
		{"mknod - S_IFCHR", "mknod;native;0,S_IFCHR", main.SeccompRetAllow},
		{"mknod - S_IFBLK", "mknod;native;0,S_IFBLK", main.SeccompRetAllow},
		{"mknod - S_IFIFO", "mknod;native;0,S_IFIFO", main.SeccompRetAllow},
		{"mknod - S_IFSOCK", "mknod;native;0,S_IFSOCK", main.SeccompRetAllow},
		{"mknod - S_IFREG", "mknod;native;0,999", main.SeccompRetKill},
		{"mknod - S_IFCHR", "mknod;native;0,999", main.SeccompRetKill},
		{"mknod - S_IFBLK", "mknod;native;0,999", main.SeccompRetKill},
		{"mknod - S_IFIFO", "mknod;native;0,999", main.SeccompRetKill},
		{"mknod - S_IFSOCK", "mknod;native;0,999", main.SeccompRetKill},
		// test_restrictions_working_args_prio
		{"setpriority PRIO_PROCESS", "setpriority;native;PRIO_PROCESS", main.SeccompRetAllow},
		{"setpriority PRIO_PGRP", "setpriority;native;PRIO_PGRP", main.SeccompRetAllow},
		{"setpriority PRIO_USER", "setpriority;native;PRIO_USER", main.SeccompRetAllow},
		{"setpriority PRIO_PROCESS", "setpriority;native;99", main.SeccompRetKill},
		{"setpriority PRIO_PGRP", "setpriority;native;99", main.SeccompRetKill},
		{"setpriority PRIO_USER", "setpriority;native;99", main.SeccompRetKill},
		// test_restrictions_working_args_termios
		{"ioctl - TIOCSTI", "ioctl;native;0,TIOCSTI", main.SeccompRetAllow},
		{"ioctl - TIOCSTI", "quotactl;native;0,99", main.SeccompRetKill},
	} {
		simulateBpf(c, t.seccompWhitelist, t.bpfInput, t.expected)
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
		{"mbind - - - - - - 7", `cannot parse line: too many arguments specified for syscall 'mbind' in line.*`},
		{"mbind 1 2 3 4 5 6 7", `cannot parse line: too many arguments specified for syscall 'mbind' in line.*`},
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

// ported from test_restrictions_working_args_socket
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsSocket(c *C) {
	bpf.VmEndianness = nativeEndian()

	for _, pre := range []string{"AF", "PF"} {
		for _, i := range []string{"UNIX", "LOCAL", "INET", "INET6", "IPX", "NETLINK", "X25", "AX25", "ATMPVC", "APPLETALK", "PACKET", "ALG", "CAN", "BRIDGE", "NETROM", "ROSE", "NETBEUI", "SECURITY", "KEY", "ASH", "ECONET", "SNA", "IRDA", "PPPOX", "WANPIPE", "BLUETOOTH", "RDS", "LLC", "TIPC", "IUCV", "RXRPC", "ISDN", "PHONET", "IEEE802154", "CAIF", "NFC", "VSOCK", "MPLS", "IB"} {
			seccompWhitelist := fmt.Sprintf("socket %s_%s", pre, i)
			bpfInputGood := fmt.Sprintf("socket;native;%s_%s", pre, i)
			bpfInputBad := "socket;native;99999"
			simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
			simulateBpf(c, seccompWhitelist, bpfInputBad, main.SeccompRetKill)

			for _, j := range []string{"SOCK_STREAM", "SOCK_DGRAM", "SOCK_SEQPACKET", "SOCK_RAW", "SOCK_RDM", "SOCK_PACKET"} {
				seccompWhitelist := fmt.Sprintf("socket %s_%s %s", pre, i, j)
				bpfInputGood := fmt.Sprintf("socket;native;%s_%s,%s", pre, i, j)
				bpfInputBad := fmt.Sprintf("socket;native;%s_%s,9999", pre, i)
				simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
				simulateBpf(c, seccompWhitelist, bpfInputBad, main.SeccompRetKill)
			}
		}
	}

	for _, i := range []string{"NETLINK_ROUTE", "NETLINK_USERSOCK", "NETLINK_FIREWALL", "NETLINK_SOCK_DIAG", "NETLINK_NFLOG", "NETLINK_XFRM", "NETLINK_SELINUX", "NETLINK_ISCSI", "NETLINK_AUDIT", "NETLINK_FIB_LOOKUP", "NETLINK_CONNECTOR", "NETLINK_NETFILTER", "NETLINK_IP6_FW", "NETLINK_DNRTMSG", "NETLINK_KOBJECT_UEVENT", "NETLINK_GENERIC", "NETLINK_SCSITRANSPORT", "NETLINK_ECRYPTFS", "NETLINK_RDMA", "NETLINK_CRYPTO", "NETLINK_INET_DIAG"} {
		for _, j := range []string{"AF_NETLINK", "PF_NETLINK"} {
			seccompWhitelist := fmt.Sprintf("socket %s - %s", j, i)
			bpfInputGood := fmt.Sprintf("socket;native;%s,0,%s", j, i)
			bpfInputBad := fmt.Sprintf("socket;native;%s,0,99", j)
			simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
			simulateBpf(c, seccompWhitelist, bpfInputBad, main.SeccompRetKill)
		}
	}
}

// ported from test_restrictions_working_args_quotactl
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsQuotactl(c *C) {
	bpf.VmEndianness = nativeEndian()

	for _, arg := range []string{"Q_QUOTAON", "Q_QUOTAOFF", "Q_GETQUOTA", "Q_SETQUOTA", "Q_GETINFO", "Q_SETINFO", "Q_GETFMT"} {
		seccompWhitelist := fmt.Sprintf("quotactl %s", arg)
		bpfInputGood := fmt.Sprintf("quotactl;native;%s", arg)
		simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
		for _, bad := range []string{"quotactl;native;99999", "read;native;"} {
			simulateBpf(c, seccompWhitelist, bad, main.SeccompRetKill)
		}
	}
}

// ported from test_restrictions_working_args_prctl
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsPrctl(c *C) {
	bpf.VmEndianness = nativeEndian()

	for _, arg := range []string{"PR_CAP_AMBIENT", "PR_CAP_AMBIENT_RAISE", "PR_CAP_AMBIENT_LOWER", "PR_CAP_AMBIENT_IS_SET", "PR_CAP_AMBIENT_CLEAR_ALL", "PR_CAPBSET_READ", "PR_CAPBSET_DROP", "PR_SET_CHILD_SUBREAPER", "PR_GET_CHILD_SUBREAPER", "PR_SET_DUMPABLE", "PR_GET_DUMPABLE", "PR_SET_ENDIAN", "PR_GET_ENDIAN", "PR_SET_FPEMU", "PR_GET_FPEMU", "PR_SET_FPEXC", "PR_GET_FPEXC", "PR_SET_KEEPCAPS", "PR_GET_KEEPCAPS", "PR_MCE_KILL", "PR_MCE_KILL_GET", "PR_SET_MM", "PR_SET_MM_START_CODE", "PR_SET_MM_END_CODE", "PR_SET_MM_START_DATA", "PR_SET_MM_END_DATA", "PR_SET_MM_START_STACK", "PR_SET_MM_START_BRK", "PR_SET_MM_BRK", "PR_SET_MM_ARG_START", "PR_SET_MM_ARG_END", "PR_SET_MM_ENV_START", "PR_SET_MM_ENV_END", "PR_SET_MM_AUXV", "PR_SET_MM_EXE_FILE", "PR_MPX_ENABLE_MANAGEMENT", "PR_MPX_DISABLE_MANAGEMENT", "PR_SET_NAME", "PR_GET_NAME", "PR_SET_NO_NEW_PRIVS", "PR_GET_NO_NEW_PRIVS", "PR_SET_PDEATHSIG", "PR_GET_PDEATHSIG", "PR_SET_PTRACER", "PR_SET_SECCOMP", "PR_GET_SECCOMP", "PR_SET_SECUREBITS", "PR_GET_SECUREBITS", "PR_SET_THP_DISABLE", "PR_TASK_PERF_EVENTS_DISABLE", "PR_TASK_PERF_EVENTS_ENABLE", "PR_GET_THP_DISABLE", "PR_GET_TID_ADDRESS", "PR_SET_TIMERSLACK", "PR_GET_TIMERSLACK", "PR_SET_TIMING", "PR_GET_TIMING", "PR_SET_TSC", "PR_GET_TSC", "PR_SET_UNALIGN", "PR_GET_UNALIGN"} {
		seccompWhitelist := fmt.Sprintf("prctl %s", arg)
		bpfInputGood := fmt.Sprintf("prctl;native;%s", arg)
		simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
		for _, bad := range []string{"prctl;native;99999", "read;native;"} {
			simulateBpf(c, seccompWhitelist, bad, main.SeccompRetKill)
		}

		if arg == "PR_CAP_AMBIENT" {
			for _, j := range []string{"PR_CAP_AMBIENT_RAISE", "PR_CAP_AMBIENT_LOWER", "PR_CAP_AMBIENT_IS_SET", "PR_CAP_AMBIENT_CLEAR_ALL"} {
				seccompWhitelist := fmt.Sprintf("prctl %s %s", arg, j)
				bpfInputGood := fmt.Sprintf("prctl;native;%s,%s", arg, j)
				simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
				for _, bad := range []string{
					fmt.Sprintf("prctl;native;%s,99999", arg),
					"read;native;",
				} {
					simulateBpf(c, seccompWhitelist, bad, main.SeccompRetKill)
				}
			}
		}
	}
}
