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
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	. "gopkg.in/check.v1"

	"github.com/mvo5/libseccomp-golang"

	// forked from "golang.org/x/net/bpf"
	// until https://github.com/golang/go/issues/20556
	"github.com/mvo5/net/bpf"

	"github.com/snapcore/snapd/arch"
	main "github.com/snapcore/snapd/cmd/snap-seccomp"
	"github.com/snapcore/snapd/release"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type snapSeccompSuite struct {
	seccompBpfLoader     string
	seccompSyscallRunner string
}

var _ = Suite(&snapSeccompSuite{})

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

func parseBpfInput(s string) (*main.SeccompData, error) {
	// syscall;arch;arg1,arg2...
	l := strings.Split(s, ";")

	var scmpArch seccomp.ScmpArch
	if len(l) < 2 || l[1] == "native" {
		scmpArch = main.UbuntuArchToScmpArch(arch.UbuntuArchitecture())
	} else {
		scmpArch = main.UbuntuArchToScmpArch(l[1])
	}

	sc, err := seccomp.GetSyscallFromNameByArch(l[0], scmpArch)
	if err != nil {
		return nil, err
	}
	// libseccomp may return negative numbers here for syscalls that
	// are "special" for some reason. There is no "official" way to
	// resolve them using the API to the real number. This is why
	// we workaround there.
	if sc < 0 {
		/* -101 is __PNR_socket */
		if sc == -101 && scmpArch == seccomp.ArchX86 {
			sc = 359 /* see src/arch-x86.c socket */
		} else if sc == -101 && scmpArch == seccomp.ArchS390X {
			sc = 359 /* see src/arch-s390x.c socket */
		} else if sc == -10165 && scmpArch == seccomp.ArchARM64 {
			// -10165 is mknod on aarch64 and it is translated
			// to mknodat. For our simulation -10165 is fine
			// though.
		} else {
			panic(fmt.Sprintf("cannot resolve syscall %v for arch %v, got %v", l[0], l[1], sc))
		}
	}

	var syscallArgs [6]uint64
	if len(l) > 2 {
		args := strings.Split(l[2], ",")
		for i := range args {
			// init with random number argument to avoid
			// the test passes accidentally because every
			// argument is set to zero
			syscallArgs[i] = (uint64)(rand.Uint32())
			// override if the test specifies a specific number
			if nr, err := strconv.ParseUint(args[i], 10, 64); err == nil {
				syscallArgs[i] = nr
			} else if nr, ok := main.SeccompResolver[args[i]]; ok {
				syscallArgs[i] = nr
			}
		}
	}
	sd := &main.SeccompData{}
	sd.SetArch(main.ScmpArchToSeccompNativeArch(scmpArch))
	sd.SetNr(sc)
	sd.SetArgs(syscallArgs)
	return sd, nil
}

// Endianness detection.
func nativeEndian() binary.ByteOrder {
	// Credit Matt Kane, taken from his gosndfile project.
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

var seccompBpfLoaderContent = []byte(`
#include <fcntl.h>
#include <inttypes.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <unistd.h>

#include <linux/filter.h>
#include <linux/seccomp.h>

#define MAX_BPF_SIZE 32 * 1024

int sc_apply_seccomp_bpf(const char* profile_path)
{
    unsigned char bpf[MAX_BPF_SIZE + 1]; // account for EOF
    FILE* fp;
    fp = fopen(profile_path, "rb");
    if (fp == NULL) {
        fprintf(stderr, "cannot read %s\n", profile_path);
        exit(1);
    }

    // set 'size' to 1; to get bytes transferred
    size_t num_read = fread(bpf, 1, sizeof(bpf), fp);

    if (ferror(fp) != 0) {
        perror("fread()");
        exit(1);
    } else if (feof(fp) == 0) {
        fprintf(stderr, "file too big\n");
        exit(1);
    }
    fclose(fp);

    struct sock_fprog prog = {
        .len = num_read / sizeof(struct sock_filter),
        .filter = (struct sock_filter*)bpf,
    };

    // Set NNP to allow loading seccomp policy into the kernel without
    // root
    if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)) {
        perror("prctl(PR_NO_NEW_PRIVS, 1, 0, 0, 0)");
        exit(1);
    }

    if (prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &prog)) {
        perror("prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, ...) failed");
        exit(1);
    }
    return 0;
}

int main(int argc, char* argv[])
{
    int rc = 0;
    if (argc < 2) {
        fprintf(stderr, "Usage: %s <bpf file> [prog ...]\n", argv[0]);
        return 1;
    }

    rc = sc_apply_seccomp_bpf(argv[1]);
    if (rc || argc == 2)
        return rc;

    execv(argv[2], (char* const*)&argv[2]);
    perror("execv failed");
    return 1;
}
`)

var seccompSyscallRunnerContent = []byte(`
#define _GNU_SOURCE
#include <stdlib.h>
#include <sys/syscall.h>
#include <unistd.h>
int main(int argc, char** argv)
{
    int l[7];
    for (int i = 0; i < 7; i++)
        l[i] = atoi(argv[i + 1]);
    // There might be architecture-specific requirements. see "man syscall"
    // for details.
    syscall(l[0], l[1], l[2], l[3], l[4], l[5], l[6]);
    syscall(SYS_exit, 0, 0, 0, 0, 0, 0);
}
`)

func lastKmsg() string {
	output, err := exec.Command("dmesg").CombinedOutput()
	if err != nil {
		return err.Error()
	}
	l := strings.Split(string(output), "\n")
	return fmt.Sprintf("Showing last 10 lines of dmesg:\n%s", strings.Join(l[len(l)-10:], "\n"))
}

func (s *snapSeccompSuite) SetUpSuite(c *C) {
	// FIXME: we currently use a fork of x/net/bpf because of:
	//   https://github.com/golang/go/issues/20556
	// switch to x/net/bpf once we can simulate seccomp bpf there
	bpf.VmEndianness = nativeEndian()

	// build seccomp-load helper
	s.seccompBpfLoader = filepath.Join(c.MkDir(), "seccomp_bpf_loader")
	err := ioutil.WriteFile(s.seccompBpfLoader+".c", seccompBpfLoaderContent, 0644)
	c.Assert(err, IsNil)
	cmd := exec.Command("gcc", "-Werror", "-Wall", s.seccompBpfLoader+".c", "-o", s.seccompBpfLoader)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	c.Assert(err, IsNil)

	// build syscall-runner helper
	s.seccompSyscallRunner = filepath.Join(c.MkDir(), "seccomp_syscall_runner")
	err = ioutil.WriteFile(s.seccompSyscallRunner+".c", seccompSyscallRunnerContent, 0644)
	c.Assert(err, IsNil)
	cmd = exec.Command("gcc", "-Werror", "-Wall", "-static", s.seccompSyscallRunner+".c", "-o", s.seccompSyscallRunner, "-Wl,-static", "-static-libgcc")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	c.Assert(err, IsNil)
}

func (s *snapSeccompSuite) runBpfInKernel(c *C, seccompWhitelist, bpfInput string, expected int) {
	// Common syscalls we need to allow for a minimal statically linked
	// c program.
	//
	// If we compile a test program for each test we can get away with
	// a even smaller set of syscalls: execve,exit essentially. But it
	// means a much longer test run (30s vs 2s). Commit d288d89 contains
	// the code for this.
	common := `
execve
uname
brk
arch_prctl
readlink
access
sysinfo
exit
# i386
set_thread_area
`
	bpfPath := filepath.Join(c.MkDir(), "bpf")
	err := main.Compile([]byte(common+seccompWhitelist), bpfPath)
	c.Assert(err, IsNil)

	// syscallName;arch;arg1,arg2...
	l := strings.Split(bpfInput, ";")
	if len(l) > 1 && l[1] != "native" {
		c.Logf("cannot use non-native in runBpfInKernel")
		return
	}
	// Skip prctl(PR_SET_ENDIAN) that causes havoc when run.
	//
	// Note that we will need to also skip: fadvise64_64,
	//   ftruncate64, posix_fadvise, pread64, pwrite64, readahead,
	//   sync_file_range, and truncate64.
	// Once we start using those. See `man syscall`
	if strings.Contains(bpfInput, "PR_SET_ENDIAN") {
		c.Logf("cannot run PR_SET_ENDIAN in runBpfInKernel, this actually switches the endianess and the program crashes")
		return
	}

	var syscallRunnerArgs [7]string
	syscallNr, err := seccomp.GetSyscallFromName(l[0])
	c.Assert(err, IsNil)
	syscallRunnerArgs[0] = strconv.FormatInt(int64(syscallNr), 10)
	if len(l) > 2 {
		args := strings.Split(l[2], ",")
		for i := range args {
			// init with random number argument
			syscallArg := (uint64)(rand.Uint32())
			// override if the test specifies a specific number
			if nr, err := strconv.ParseUint(args[i], 10, 64); err == nil {
				syscallArg = nr
			} else if nr, ok := main.SeccompResolver[args[i]]; ok {
				syscallArg = nr
			}
			syscallRunnerArgs[i+1] = strconv.FormatUint(syscallArg, 10)
		}
	}

	cmd := exec.Command(s.seccompBpfLoader, bpfPath, s.seccompSyscallRunner, syscallRunnerArgs[0], syscallRunnerArgs[1], syscallRunnerArgs[2], syscallRunnerArgs[3], syscallRunnerArgs[4], syscallRunnerArgs[5], syscallRunnerArgs[6])
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	switch expected {
	case main.SeccompRetAllow:
		if err != nil {
			c.Fatalf("unexpected error for %q (failed to run %q): %s", seccompWhitelist, lastKmsg(), err)
		}
	case main.SeccompRetKill:
		if err == nil {
			c.Fatalf("unexpected success for %q %q (ran but should have failed %s)", seccompWhitelist, bpfInput, lastKmsg())
		}
	default:
		c.Fatalf("unknown expected result %v", expected)
	}
}

// simulateBpf first:
//  1. runs main.Compile() which will catch syntax errors and output to a file
//  2. takes the output file from main.Compile and loads it via
//     decodeBpfFromFile
//  3. parses the decoded bpf using the seccomp library and various
//     snapd functions
//  4. runs the parsed bpf through a bpf VM
//
// Then simulateBpf runs the policy through the kernel by calling
// runBpfInKernel() which:
//  1. runs main.Compile()
//  2. the program in seccompBpfLoaderContent with the output file as an
//     argument
//  3. the program in seccompBpfLoaderContent loads the output file BPF into
//     the kernel and executes the program in seccompBpfRunnerContent with the
//     syscall and arguments specified by the test
//
// In this manner, in addition to verifying policy syntax we are able to
// unit test the resulting bpf in several ways.
//
// Full testing of applied policy is done elsewhere via spread tests.
func (s *snapSeccompSuite) simulateBpf(c *C, seccompWhitelist, bpfInput string, expected int) {
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

	s.runBpfInKernel(c, seccompWhitelist, bpfInput, expected)
}

func systemUsesSocketcall() bool {
	// We need to skip the tests on trusty/i386 and trusty/s390x as
	// those are using the socketcall syscall instead of the real
	// socket syscall.
	//
	// See also:
	// https://bugs.launchpad.net/ubuntu/+source/glibc/+bug/1576066
	if release.ReleaseInfo.VersionID == "14.04" {
		if arch.UbuntuArchitecture() == "i386" || arch.UbuntuArchitecture() == "s390x" {
			return true
		}
	}
	return false
}

// TestCompile iterates over a range of textual seccomp whitelist rules and
// mocked kernel syscall input. For each rule, the test consists of compiling
// the rule into a bpf program and then running that program on a virtual bpf
// machine and comparing the bpf machine output to the specified expected
// output and seccomp operation. Eg:
//    {"<rule>", "<mocked kernel input>", <seccomp result>}
//
// Eg to test that the rule 'read >=2' is allowed with 'read(2)' and 'read(3)'
// and denied with 'read(1)' and 'read(0)', add the following tests:
//    {"read >=2", "read;native;2", main.SeccompRetAllow},
//    {"read >=2", "read;native;3", main.SeccompRetAllow},
//    {"read >=2", "read;native;1", main.SeccompRetKill},
//    {"read >=2", "read;native;0", main.SeccompRetKill},
func (s *snapSeccompSuite) TestCompile(c *C) {
	// The 'shadow' group is different in different distributions
	shadowGid, err := main.FindGid("shadow")
	c.Assert(err, IsNil)

	for _, t := range []struct {
		seccompWhitelist string
		bpfInput         string
		expected         int
	}{
		// special
		{"@unrestricted", "execve", main.SeccompRetAllow},
		{"@complain", "execve", main.SeccompRetAllow},

		// trivial allow
		{"read", "read", main.SeccompRetAllow},
		{"read\nwrite\nexecve\n", "write", main.SeccompRetAllow},

		// trivial denial
		{"read", "ioctl", main.SeccompRetKill},

		// test argument filtering syntax, we currently support:
		//   >=, <=, !, <, >, |
		// modifiers.

		// reads >= 2 are ok
		{"read >=2", "read;native;2", main.SeccompRetAllow},
		{"read >=2", "read;native;3", main.SeccompRetAllow},
		// but not reads < 2, those get killed
		{"read >=2", "read;native;1", main.SeccompRetKill},
		{"read >=2", "read;native;0", main.SeccompRetKill},

		// reads <= 2 are ok
		{"read <=2", "read;native;0", main.SeccompRetAllow},
		{"read <=2", "read;native;1", main.SeccompRetAllow},
		{"read <=2", "read;native;2", main.SeccompRetAllow},
		// but not reads >2, those get killed
		{"read <=2", "read;native;3", main.SeccompRetKill},
		{"read <=2", "read;native;4", main.SeccompRetKill},

		// reads that are not 2 are ok
		{"read !2", "read;native;1", main.SeccompRetAllow},
		{"read !2", "read;native;3", main.SeccompRetAllow},
		// but not 2, this gets killed
		{"read !2", "read;native;2", main.SeccompRetKill},

		// reads > 2 are ok
		{"read >2", "read;native;4", main.SeccompRetAllow},
		{"read >2", "read;native;3", main.SeccompRetAllow},
		// but not reads <= 2, those get killed
		{"read >2", "read;native;2", main.SeccompRetKill},
		{"read >2", "read;native;1", main.SeccompRetKill},

		// reads < 2 are ok
		{"read <2", "read;native;0", main.SeccompRetAllow},
		{"read <2", "read;native;1", main.SeccompRetAllow},
		// but not reads >= 2, those get killed
		{"read <2", "read;native;2", main.SeccompRetKill},
		{"read <2", "read;native;3", main.SeccompRetKill},

		// FIXME: test maskedEqual better
		{"read |1", "read;native;1", main.SeccompRetAllow},
		{"read |1", "read;native;2", main.SeccompRetKill},

		// exact match, reads == 2 are ok
		{"read 2", "read;native;2", main.SeccompRetAllow},
		// but not those != 2
		{"read 2", "read;native;3", main.SeccompRetKill},
		{"read 2", "read;native;1", main.SeccompRetKill},

		// test actual syscalls and their expected usage
		{"ioctl - TIOCSTI", "ioctl;native;-,TIOCSTI", main.SeccompRetAllow},
		{"ioctl - TIOCSTI", "ioctl;native;-,99", main.SeccompRetKill},
		{"ioctl - !TIOCSTI", "ioctl;native;-,TIOCSTI", main.SeccompRetKill},

		// test_bad_seccomp_filter_args_clone
		{"setns - CLONE_NEWNET", "setns;native;-,99", main.SeccompRetKill},
		{"setns - CLONE_NEWNET", "setns;native;-,CLONE_NEWNET", main.SeccompRetAllow},

		// test_bad_seccomp_filter_args_mknod
		{"mknod - |S_IFIFO", "mknod;native;-,S_IFIFO", main.SeccompRetAllow},
		{"mknod - |S_IFIFO", "mknod;native;-,99", main.SeccompRetKill},

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
		{"socket - SOCK_STREAM", "socket;native;-,SOCK_STREAM", main.SeccompRetAllow},
		{"socket - SOCK_STREAM", "socket;native;-,99", main.SeccompRetKill},

		// test_bad_seccomp_filter_args_termios
		{"ioctl - TIOCSTI", "ioctl;native;-,TIOCSTI", main.SeccompRetAllow},
		{"ioctl - TIOCSTI", "ioctl;native;-,99", main.SeccompRetKill},

		// u:root g:shadow
		{"fchown - u:root g:shadow", fmt.Sprintf("fchown;native;-,0,%d", shadowGid), main.SeccompRetAllow},
		{"fchown - u:root g:shadow", fmt.Sprintf("fchown;native;-,99,%d", shadowGid), main.SeccompRetKill},
		{"chown - u:root g:shadow", fmt.Sprintf("chown;native;-,0,%d", shadowGid), main.SeccompRetAllow},
		{"chown - u:root g:shadow", fmt.Sprintf("chown;native;-,99,%d", shadowGid), main.SeccompRetKill},
	} {
		// skip socket tests if the system uses socketcall instead
		// of socket
		if strings.Contains(t.seccompWhitelist, "socket") && systemUsesSocketcall() {
			continue
		}
		s.simulateBpf(c, t.seccompWhitelist, t.bpfInput, t.expected)
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
		{"setpriority bar", `cannot parse line: cannot parse token "bar" .*`},
		{"setpriority -1", `cannot parse line: cannot parse token "-1" .*`},
		{"setpriority 0 - -1 0", `cannot parse line: cannot parse token "-1" .*`},
		{"setpriority --10", `cannot parse line: cannot parse token "--10" .*`},
		{"setpriority 0:10", `cannot parse line: cannot parse token "0:10" .*`},
		{"setpriority 0-10", `cannot parse line: cannot parse token "0-10" .*`},
		{"setpriority 0,1", `cannot parse line: cannot parse token "0,1" .*`},
		{"setpriority 0x0", `cannot parse line: cannot parse token "0x0" .*`},
		{"setpriority a1", `cannot parse line: cannot parse token "a1" .*`},
		{"setpriority 1a", `cannot parse line: cannot parse token "1a" .*`},
		{"setpriority 1-", `cannot parse line: cannot parse token "1-" .*`},
		{"setpriority 1\\ 2", `cannot parse line: cannot parse token "1\\\\" .*`},
		{"setpriority 1\\n2", `cannot parse line: cannot parse token "1\\\\n2" .*`},
		{"setpriority 999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999", `cannot parse line: cannot parse token "999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999" .*`},
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
		{"socket - NETLINK_ROUT", `cannot parse line: cannot parse token "NETLINK_ROUT" .*`},
		{"socket - NETLINK_ROUTEE", `cannot parse line: cannot parse token "NETLINK_ROUTEE" .*`},
		{"socket - NETLINK_R0UTE", `cannot parse line: cannot parse token "NETLINK_R0UTE" .*`},
		// test_bad_seccomp_filter_args_termios
		{"ioctl - TIOCST", `cannot parse line: cannot parse token "TIOCST" .*`},
		{"ioctl - TIOCSTII", `cannot parse line: cannot parse token "TIOCSTII" .*`},
		{"ioctl - TIOCST1", `cannot parse line: cannot parse token "TIOCST1" .*`},
		// ensure missing numbers are caught
		{"setpriority >", `cannot parse line: cannot parse token ">" .*`},
		{"setpriority >=", `cannot parse line: cannot parse token ">=" .*`},
		{"setpriority <", `cannot parse line: cannot parse token "<" .*`},
		{"setpriority <=", `cannot parse line: cannot parse token "<=" .*`},
		{"setpriority |", `cannot parse line: cannot parse token "|" .*`},
		{"setpriority !", `cannot parse line: cannot parse token "!" .*`},

		// u:<username>
		{"setuid :root", `cannot parse line: cannot parse token ":root" .*`},
		{"setuid u:", `cannot parse line: cannot parse token "u:" \(line "setuid u:"\): "" must be a valid username`},
		{"setuid u:0", `cannot parse line: cannot parse token "u:0" \(line "setuid u:0"\): "0" must be a valid username`},
		{"setuid u:b@d|npu+", `cannot parse line: cannot parse token "u:b@d|npu+" \(line "setuid u:b@d|npu+"\): "b@d|npu+" must be a valid username`},
		{"setuid u:snap.bad", `cannot parse line: cannot parse token "u:snap.bad" \(line "setuid u:snap.bad"\): "snap.bad" must be a valid username`},
		{"setuid U:root", `cannot parse line: cannot parse token "U:root" .*`},
		{"setuid u:nonexistent", `cannot parse line: cannot parse token "u:nonexistent" \(line "setuid u:nonexistent"\): user: unknown user nonexistent`},
		// g:<groupname>
		{"setgid g:", `cannot parse line: cannot parse token "g:" \(line "setgid g:"\): "" must be a valid group name`},
		{"setgid g:0", `cannot parse line: cannot parse token "g:0" \(line "setgid g:0"\): "0" must be a valid group name`},
		{"setgid g:b@d|npu+", `cannot parse line: cannot parse token "g:b@d|npu+" \(line "setgid g:b@d|npu+"\): "b@d|npu+" must be a valid group name`},
		{"setgid g:snap.bad", `cannot parse line: cannot parse token "g:snap.bad" \(line "setgid g:snap.bad"\): "snap.bad" must be a valid group name`},
		{"setgid G:root", `cannot parse line: cannot parse token "G:root" .*`},
		{"setgid g:nonexistent", `cannot parse line: cannot parse token "g:nonexistent" \(line "setgid g:nonexistent"\): group: unknown group nonexistent`},
	} {
		outPath := filepath.Join(c.MkDir(), "bpf")
		err := main.Compile([]byte(t.inp), outPath)
		c.Check(err, ErrorMatches, t.errMsg, Commentf("%q errors in unexpected ways, got: %q expected %q", t.inp, err, t.errMsg))
	}
}

// ported from test_restrictions_working_args_socket
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsSocket(c *C) {
	// skip socket tests if the system uses socketcall instead
	// of socket
	if systemUsesSocketcall() {
		c.Skip("cannot run when socketcall() is used")
		return
	}

	for _, pre := range []string{"AF", "PF"} {
		for _, i := range []string{"UNIX", "LOCAL", "INET", "INET6", "IPX", "NETLINK", "X25", "AX25", "ATMPVC", "APPLETALK", "PACKET", "ALG", "CAN", "BRIDGE", "NETROM", "ROSE", "NETBEUI", "SECURITY", "KEY", "ASH", "ECONET", "SNA", "IRDA", "PPPOX", "WANPIPE", "BLUETOOTH", "RDS", "LLC", "TIPC", "IUCV", "RXRPC", "ISDN", "PHONET", "IEEE802154", "CAIF", "NFC", "VSOCK", "MPLS", "IB"} {
			seccompWhitelist := fmt.Sprintf("socket %s_%s", pre, i)
			bpfInputGood := fmt.Sprintf("socket;native;%s_%s", pre, i)
			bpfInputBad := "socket;native;99999"
			s.simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
			s.simulateBpf(c, seccompWhitelist, bpfInputBad, main.SeccompRetKill)

			for _, j := range []string{"SOCK_STREAM", "SOCK_DGRAM", "SOCK_SEQPACKET", "SOCK_RAW", "SOCK_RDM", "SOCK_PACKET"} {
				seccompWhitelist := fmt.Sprintf("socket %s_%s %s", pre, i, j)
				bpfInputGood := fmt.Sprintf("socket;native;%s_%s,%s", pre, i, j)
				bpfInputBad := fmt.Sprintf("socket;native;%s_%s,9999", pre, i)
				s.simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
				s.simulateBpf(c, seccompWhitelist, bpfInputBad, main.SeccompRetKill)
			}
		}
	}

	for _, i := range []string{"NETLINK_ROUTE", "NETLINK_USERSOCK", "NETLINK_FIREWALL", "NETLINK_SOCK_DIAG", "NETLINK_NFLOG", "NETLINK_XFRM", "NETLINK_SELINUX", "NETLINK_ISCSI", "NETLINK_AUDIT", "NETLINK_FIB_LOOKUP", "NETLINK_CONNECTOR", "NETLINK_NETFILTER", "NETLINK_IP6_FW", "NETLINK_DNRTMSG", "NETLINK_KOBJECT_UEVENT", "NETLINK_GENERIC", "NETLINK_SCSITRANSPORT", "NETLINK_ECRYPTFS", "NETLINK_RDMA", "NETLINK_CRYPTO", "NETLINK_INET_DIAG"} {
		for _, j := range []string{"AF_NETLINK", "PF_NETLINK"} {
			seccompWhitelist := fmt.Sprintf("socket %s - %s", j, i)
			bpfInputGood := fmt.Sprintf("socket;native;%s,0,%s", j, i)
			bpfInputBad := fmt.Sprintf("socket;native;%s,0,99", j)
			s.simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
			s.simulateBpf(c, seccompWhitelist, bpfInputBad, main.SeccompRetKill)
		}
	}
}

// ported from test_restrictions_working_args_quotactl
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsQuotactl(c *C) {
	for _, arg := range []string{"Q_QUOTAON", "Q_QUOTAOFF", "Q_GETQUOTA", "Q_SETQUOTA", "Q_GETINFO", "Q_SETINFO", "Q_GETFMT", "Q_SYNC", "Q_XQUOTAON", "Q_XQUOTAOFF", "Q_XGETQUOTA", "Q_XSETQLIM", "Q_XGETQSTAT", "Q_XQUOTARM"} {
		// good input
		seccompWhitelist := fmt.Sprintf("quotactl %s", arg)
		bpfInputGood := fmt.Sprintf("quotactl;native;%s", arg)
		s.simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
		// bad input
		for _, bad := range []string{"quotactl;native;99999", "read;native;"} {
			s.simulateBpf(c, seccompWhitelist, bad, main.SeccompRetKill)
		}
	}
}

// ported from test_restrictions_working_args_prctl
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsPrctl(c *C) {
	bpf.VmEndianness = nativeEndian()

	for _, arg := range []string{"PR_CAP_AMBIENT", "PR_CAP_AMBIENT_RAISE", "PR_CAP_AMBIENT_LOWER", "PR_CAP_AMBIENT_IS_SET", "PR_CAP_AMBIENT_CLEAR_ALL", "PR_CAPBSET_READ", "PR_CAPBSET_DROP", "PR_SET_CHILD_SUBREAPER", "PR_GET_CHILD_SUBREAPER", "PR_SET_DUMPABLE", "PR_GET_DUMPABLE", "PR_SET_ENDIAN", "PR_GET_ENDIAN", "PR_SET_FPEMU", "PR_GET_FPEMU", "PR_SET_FPEXC", "PR_GET_FPEXC", "PR_SET_KEEPCAPS", "PR_GET_KEEPCAPS", "PR_MCE_KILL", "PR_MCE_KILL_GET", "PR_SET_MM", "PR_SET_MM_START_CODE", "PR_SET_MM_END_CODE", "PR_SET_MM_START_DATA", "PR_SET_MM_END_DATA", "PR_SET_MM_START_STACK", "PR_SET_MM_START_BRK", "PR_SET_MM_BRK", "PR_SET_MM_ARG_START", "PR_SET_MM_ARG_END", "PR_SET_MM_ENV_START", "PR_SET_MM_ENV_END", "PR_SET_MM_AUXV", "PR_SET_MM_EXE_FILE", "PR_MPX_ENABLE_MANAGEMENT", "PR_MPX_DISABLE_MANAGEMENT", "PR_SET_NAME", "PR_GET_NAME", "PR_SET_NO_NEW_PRIVS", "PR_GET_NO_NEW_PRIVS", "PR_SET_PDEATHSIG", "PR_GET_PDEATHSIG", "PR_SET_PTRACER", "PR_SET_SECCOMP", "PR_GET_SECCOMP", "PR_SET_SECUREBITS", "PR_GET_SECUREBITS", "PR_SET_THP_DISABLE", "PR_TASK_PERF_EVENTS_DISABLE", "PR_TASK_PERF_EVENTS_ENABLE", "PR_GET_THP_DISABLE", "PR_GET_TID_ADDRESS", "PR_SET_TIMERSLACK", "PR_GET_TIMERSLACK", "PR_SET_TIMING", "PR_GET_TIMING", "PR_SET_TSC", "PR_GET_TSC", "PR_SET_UNALIGN", "PR_GET_UNALIGN"} {
		// good input
		seccompWhitelist := fmt.Sprintf("prctl %s", arg)
		bpfInputGood := fmt.Sprintf("prctl;native;%s", arg)
		s.simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
		// bad input
		for _, bad := range []string{"prctl;native;99999", "setpriority;native;"} {
			s.simulateBpf(c, seccompWhitelist, bad, main.SeccompRetKill)
		}

		if arg == "PR_CAP_AMBIENT" {
			for _, j := range []string{"PR_CAP_AMBIENT_RAISE", "PR_CAP_AMBIENT_LOWER", "PR_CAP_AMBIENT_IS_SET", "PR_CAP_AMBIENT_CLEAR_ALL"} {
				seccompWhitelist := fmt.Sprintf("prctl %s %s", arg, j)
				bpfInputGood := fmt.Sprintf("prctl;native;%s,%s", arg, j)
				s.simulateBpf(c, seccompWhitelist, bpfInputGood, main.SeccompRetAllow)
				for _, bad := range []string{
					fmt.Sprintf("prctl;native;%s,99999", arg),
					"setpriority;native;",
				} {
					s.simulateBpf(c, seccompWhitelist, bad, main.SeccompRetKill)
				}
			}
		}
	}
}

// ported from test_restrictions_working_args_clone
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsClone(c *C) {
	for _, t := range []struct {
		seccompWhitelist string
		bpfInput         string
		expected         int
	}{
		// good input
		{"setns - CLONE_NEWIPC", "setns;native;-,CLONE_NEWIPC", main.SeccompRetAllow},
		{"setns - CLONE_NEWNET", "setns;native;-,CLONE_NEWNET", main.SeccompRetAllow},
		{"setns - CLONE_NEWNS", "setns;native;-,CLONE_NEWNS", main.SeccompRetAllow},
		{"setns - CLONE_NEWPID", "setns;native;-,CLONE_NEWPID", main.SeccompRetAllow},
		{"setns - CLONE_NEWUSER", "setns;native;-,CLONE_NEWUSER", main.SeccompRetAllow},
		{"setns - CLONE_NEWUTS", "setns;native;-,CLONE_NEWUTS", main.SeccompRetAllow},
		// bad input
		{"setns - CLONE_NEWIPC", "setns;native;-,99", main.SeccompRetKill},
		{"setns - CLONE_NEWNET", "setns;native;-,99", main.SeccompRetKill},
		{"setns - CLONE_NEWNS", "setns;native;-,99", main.SeccompRetKill},
		{"setns - CLONE_NEWPID", "setns;native;-,99", main.SeccompRetKill},
		{"setns - CLONE_NEWUSER", "setns;native;-,99", main.SeccompRetKill},
		{"setns - CLONE_NEWUTS", "setns;native;-,99", main.SeccompRetKill},
	} {
		s.simulateBpf(c, t.seccompWhitelist, t.bpfInput, t.expected)
	}
}

// ported from test_restrictions_working_args_mknod
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsMknod(c *C) {
	for _, t := range []struct {
		seccompWhitelist string
		bpfInput         string
		expected         int
	}{
		// good input
		{"mknod - S_IFREG", "mknod;native;-,S_IFREG", main.SeccompRetAllow},
		{"mknod - S_IFCHR", "mknod;native;-,S_IFCHR", main.SeccompRetAllow},
		{"mknod - S_IFBLK", "mknod;native;-,S_IFBLK", main.SeccompRetAllow},
		{"mknod - S_IFIFO", "mknod;native;-,S_IFIFO", main.SeccompRetAllow},
		{"mknod - S_IFSOCK", "mknod;native;-,S_IFSOCK", main.SeccompRetAllow},
		// bad input
		{"mknod - S_IFREG", "mknod;native;-,999", main.SeccompRetKill},
		{"mknod - S_IFCHR", "mknod;native;-,999", main.SeccompRetKill},
		{"mknod - S_IFBLK", "mknod;native;-,999", main.SeccompRetKill},
		{"mknod - S_IFIFO", "mknod;native;-,999", main.SeccompRetKill},
		{"mknod - S_IFSOCK", "mknod;native;-,999", main.SeccompRetKill},
	} {
		s.simulateBpf(c, t.seccompWhitelist, t.bpfInput, t.expected)
	}
}

// ported from test_restrictions_working_args_prio
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsPrio(c *C) {
	for _, t := range []struct {
		seccompWhitelist string
		bpfInput         string
		expected         int
	}{
		// good input
		{"setpriority PRIO_PROCESS", "setpriority;native;PRIO_PROCESS", main.SeccompRetAllow},
		{"setpriority PRIO_PGRP", "setpriority;native;PRIO_PGRP", main.SeccompRetAllow},
		{"setpriority PRIO_USER", "setpriority;native;PRIO_USER", main.SeccompRetAllow},
		// bad input
		{"setpriority PRIO_PROCESS", "setpriority;native;99", main.SeccompRetKill},
		{"setpriority PRIO_PGRP", "setpriority;native;99", main.SeccompRetKill},
		{"setpriority PRIO_USER", "setpriority;native;99", main.SeccompRetKill},
	} {
		s.simulateBpf(c, t.seccompWhitelist, t.bpfInput, t.expected)
	}
}

// ported from test_restrictions_working_args_termios
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsTermios(c *C) {
	for _, t := range []struct {
		seccompWhitelist string
		bpfInput         string
		expected         int
	}{
		// good input
		{"ioctl - TIOCSTI", "ioctl;native;-,TIOCSTI", main.SeccompRetAllow},
		// bad input
		{"ioctl - TIOCSTI", "quotactl;native;-,99", main.SeccompRetKill},
	} {
		s.simulateBpf(c, t.seccompWhitelist, t.bpfInput, t.expected)
	}
}

func (s *snapSeccompSuite) TestRestrictionsWorkingArgsUidGid(c *C) {
	for _, t := range []struct {
		seccompWhitelist string
		bpfInput         string
		expected         int
	}{
		// good input. 'root' and 'daemon' are guaranteed to be '0' and
		// '1' respectively
		{"setuid u:root", "setuid;native;0", main.SeccompRetAllow},
		{"setuid u:daemon", "setuid;native;1", main.SeccompRetAllow},
		{"setgid g:root", "setgid;native;0", main.SeccompRetAllow},
		{"setgid g:daemon", "setgid;native;1", main.SeccompRetAllow},
		// bad input
		{"setuid u:root", "setuid;native;99", main.SeccompRetKill},
		{"setuid u:daemon", "setuid;native;99", main.SeccompRetKill},
		{"setgid g:root", "setgid;native;99", main.SeccompRetKill},
		{"setgid g:daemon", "setgid;native;99", main.SeccompRetKill},
	} {
		s.simulateBpf(c, t.seccompWhitelist, t.bpfInput, t.expected)
	}
}

func (s *snapSeccompSuite) TestCompatArchWorks(c *C) {
	for _, t := range []struct {
		arch             string
		seccompWhitelist string
		bpfInput         string
		expected         int
	}{
		// on amd64 we add compat i386
		{"amd64", "read", "read;i386", main.SeccompRetAllow},
		{"amd64", "read", "read;amd64", main.SeccompRetAllow},
		// on arm64 we add compat armhf
		{"arm64", "read", "read;armhf", main.SeccompRetAllow},
		{"arm64", "read", "read;arm64", main.SeccompRetAllow},
		// on ppc64 we add compat powerpc
		{"ppc64", "read", "read;powerpc", main.SeccompRetAllow},
		{"ppc64", "read", "read;ppc64", main.SeccompRetAllow},
	} {
		// It is tricky to mock the architecture here because
		// seccomp is always adding the native arch to the seccomp
		// filter and it will silently discard arches that have
		// an endian mismatch:
		// https://github.com/seccomp/libseccomp/issues/86
		//
		// This means we can not just
		//    main.MockArchUbuntuArchitecture(t.arch)
		// here because on endian mismatch the arch will *not* be
		// added
		if arch.UbuntuArchitecture() == t.arch {
			s.simulateBpf(c, t.seccompWhitelist, t.bpfInput, t.expected)
		}
	}
}
