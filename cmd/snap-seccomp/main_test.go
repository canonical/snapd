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
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/seccomp/libseccomp-golang"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	main "github.com/snapcore/snapd/cmd/snap-seccomp"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type snapSeccompSuite struct {
	seccompBpfLoader     string
	seccompSyscallRunner string
	canCheckCompatArch   bool
}

var _ = Suite(&snapSeccompSuite{})

const (
	Deny = iota
	DenyExplicit
	Allow
)

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

// keep in sync with:
//  cmd/snap-confine/seccomp-support.c
//  main.go:scSeccompFileHeader
struct sc_seccomp_file_header {
   char header[2];
   char version;
   char unrestricted;
   char padding[4];
   uint32_t len_allow_filter;
   uint32_t len_deny_filter;
   char reserved2[112];
};

int sc_apply_seccomp_bpf(const char* profile_path)
{
    struct sc_seccomp_file_header hdr = {{0}, 0};
    unsigned char bpf_allow[MAX_BPF_SIZE + 1]; // account for EOF
    unsigned char bpf_deny[MAX_BPF_SIZE + 1]; // account for EOF
    FILE* fp;

    fp = fopen(profile_path, "rb");
    if (fp == NULL) {
        fprintf(stderr, "cannot read %s\n", profile_path);
        return -1;
    }
    fread(&hdr, 1, sizeof(struct sc_seccomp_file_header), fp);
    if (ferror(fp) != 0) {
        perror("fread() header");
        return -1;
    }
    fread(bpf_allow, 1, hdr.len_allow_filter, fp);
    if (ferror(fp) != 0) {
        perror("fread()");
        return -1;
    }
    fread(bpf_deny, 1, hdr.len_deny_filter, fp);
    if (ferror(fp) != 0) {
        perror("fread()");
        return -1;
    }

    fclose(fp);

    struct sock_fprog prog_allow = {
        .len = hdr.len_allow_filter / sizeof(struct sock_filter),
        .filter = (struct sock_filter*)bpf_allow,
    };

    struct sock_fprog prog_deny = {
        .len = hdr.len_deny_filter / sizeof(struct sock_filter),
        .filter = (struct sock_filter*)bpf_deny,
    };

    // Set NNP to allow loading seccomp policy into the kernel without
    // root
    if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)) {
        perror("prctl(PR_NO_NEW_PRIVS, 1, 0, 0, 0)");
        return -1;
    }

    if (prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &prog_deny)) {
        perror("prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, ...) deny failed");
        return -1;
    }
    if (prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &prog_allow)) {
        perror("prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, ...) allow failed");
        return -1;
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
    if (rc != 0)
        return -rc;

    execv(argv[2], (char* const*)&argv[2]);
    perror("execv failed");
    return 1;
}
`)

var seccompSyscallRunnerContent = []byte(`
#define _GNU_SOURCE
#include <errno.h>
#include <stdlib.h>
#include <sys/syscall.h>
#include <unistd.h>
#include <inttypes.h>
int main(int argc, char** argv)
{
    uint32_t l[7];
    int syscall_ret, ret = 0;
    for (int i = 0; i < 7 && argv[i+1] != NULL; i++) {
        errno = 0;
        l[i] = strtoll(argv[i + 1], NULL, 10);
	// exit '11' let's us know strtoll failed
        if (errno != 0)
            syscall(SYS_exit, 11, 0, 0, 0, 0, 0);
    }
    // There might be architecture-specific requirements. see "man syscall"
    // for details.
    syscall_ret = syscall(l[0], l[1], l[2], l[3], l[4], l[5], l[6]);
    // 911 is our mocked errno for implicit denials via unlisted syscalls and
    // 999 is explicit denial
    if (syscall_ret < 0 && errno == 911) {
        ret = 10;
    }
    if (syscall_ret < 0 && errno == 999) {
        ret = 20;
    }
    syscall(SYS_exit, ret, 0, 0, 0, 0, 0);
    return 0;
}
`)

func (s *snapSeccompSuite) SetUpSuite(c *C) {
	main.MockErrnoOnImplicitDenial(911)
	main.MockErrnoOnExplicitDenial(999)

	// build seccomp-load helper
	s.seccompBpfLoader = filepath.Join(c.MkDir(), "seccomp_bpf_loader")
	err := os.WriteFile(s.seccompBpfLoader+".c", seccompBpfLoaderContent, 0644)
	c.Assert(err, IsNil)
	cmd := exec.Command("gcc", "-Werror", "-Wall", s.seccompBpfLoader+".c", "-o", s.seccompBpfLoader)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	c.Assert(err, IsNil)

	// build syscall-runner helper
	s.seccompSyscallRunner = filepath.Join(c.MkDir(), "seccomp_syscall_runner")
	err = os.WriteFile(s.seccompSyscallRunner+".c", seccompSyscallRunnerContent, 0644)
	c.Assert(err, IsNil)

	cmd = exec.Command("gcc", "-std=c99", "-Werror", "-Wall", "-static", s.seccompSyscallRunner+".c", "-o", s.seccompSyscallRunner, "-Wl,-static", "-static-libgcc")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	c.Assert(err, IsNil)

	// Amazon Linux 2 is 64bit only and there is no multilib support
	s.canCheckCompatArch = !release.DistroLike("amzn")

	// Build 32bit runner on amd64 to test non-native syscall handling.
	// Ideally we would build for ppc64el->powerpc and arm64->armhf but
	// it seems tricky to find the right gcc-multilib for this.
	if arch.DpkgArchitecture() == "amd64" && s.canCheckCompatArch {
		cmd = exec.Command(cmd.Args[0], cmd.Args[1:]...)
		cmd.Args = append(cmd.Args, "-m32")
		for i, k := range cmd.Args {
			if k == s.seccompSyscallRunner {
				cmd.Args[i] = s.seccompSyscallRunner + ".m32"
			}
		}
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("cannot build multi-lib syscall runner: %v\n%s", err, output)
		}
	}
}

// Runs the policy through the kernel:
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
//
// Note that we skip testing prctl(PR_SET_ENDIAN) - it causes havoc when
// it is run. We will also need to skip: fadvise64_64, ftruncate64,
// posix_fadvise, pread64, pwrite64, readahead,	sync_file_range, and truncate64.
//
// Once we start using those. See `man syscall`
func (s *snapSeccompSuite) runBpf(c *C, seccompAllowlist, bpfInput string, expected int) {
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
# armhf
set_tls
# arm64
readlinkat
faccessat
# i386 from amd64
restart_syscall
# libc6 2.31/gcc-9.3
mprotect
`
	bpfPath := filepath.Join(c.MkDir(), "bpf")
	err := main.Compile([]byte(common+seccompAllowlist), bpfPath)
	c.Assert(err, IsNil)

	// default syscall runner
	syscallRunner := s.seccompSyscallRunner

	// syscallName;arch;arg1,arg2...
	l := strings.Split(bpfInput, ";")
	syscallName := l[0]
	syscallArch := "native"
	if len(l) > 1 {
		syscallArch = l[1]
	}

	syscallNr, err := seccomp.GetSyscallFromName(syscallName)
	c.Assert(err, IsNil)

	// Check if we want to test non-native architecture
	// handling. Doing this via the in-kernel tests is tricky as
	// we need a kernel that can run the architecture and a
	// compiler that can produce the required binaries. Currently
	// we only test amd64 running i386 here.
	if syscallArch != "native" {
		syscallNr, err = seccomp.GetSyscallFromNameByArch(syscallName, main.DpkgArchToScmpArch(syscallArch))
		c.Assert(err, IsNil)

		switch syscallArch {
		case "amd64":
			// default syscallRunner
		case "i386":
			syscallRunner = s.seccompSyscallRunner + ".m32"
		default:
			c.Errorf("unexpected non-native arch: %s", syscallArch)
		}
	}
	switch {
	case syscallNr == -101:
		// "socket" needs to be demuxed
		switch arch.DpkgArchitecture() {
		case "ppc64el":
			// see libseccomp: _ppc64_syscall_demux()
			syscallNr = 326
		default:
			// see libseccomp: _s390x_sock_demux(),
			// _x86_sock_demux() the -101 is translated to
			// 359 (socket)
			syscallNr = 359
		}
	case syscallNr == -10165:
		// "mknod" on arm64 is not available at all on arm64
		// only "mknodat" but libseccomp will not generate a
		// "mknodat" allowlist, it geneates a allowlist with
		// syscall -10165 (!?!) so we cannot test this.
		c.Skip("skipping mknod tests on arm64")
	case syscallNr < 0:
		c.Errorf("failed to resolve %v: %v", l[0], syscallNr)
		return
	}

	var syscallRunnerArgs [7]string
	syscallRunnerArgs[0] = strconv.FormatInt(int64(syscallNr), 10)
	if len(l) > 2 {
		args := strings.Split(l[2], ",")
		for i := range args {
			// init with random number argument
			syscallArg := (uint64)(rand.Uint32())
			// override if the test specifies a specific number;
			// this must match main.go:readNumber()
			if nr, ok := main.SeccompResolver[args[i]]; ok {
				syscallArg = nr
			} else if nr, err := strconv.ParseUint(args[i], 10, 32); err == nil {
				syscallArg = nr
			} else if nr, err := strconv.ParseInt(args[i], 10, 32); err == nil {
				syscallArg = uint64(uint32(nr))
			}
			syscallRunnerArgs[i+1] = strconv.FormatUint(syscallArg, 10)
		}
	}

	cmd := exec.Command(s.seccompBpfLoader, bpfPath, syscallRunner, syscallRunnerArgs[0], syscallRunnerArgs[1], syscallRunnerArgs[2], syscallRunnerArgs[3], syscallRunnerArgs[4], syscallRunnerArgs[5], syscallRunnerArgs[6])
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	// the exit code of the test binary is either 0 or 10, everything
	// else is unexpected (segv, strtoll failure, ...)
	exitCode, e := osutil.ExitCode(err)
	c.Assert(e, IsNil)
	c.Assert(exitCode == 0 || exitCode == 10 || exitCode == 20, Equals, true, Commentf("unexpected exit code: %v for %v - test setup broken", exitCode, seccompAllowlist))
	switch expected {
	case Allow:
		if err != nil {
			c.Fatalf("unexpected error for %q (failed to run %q)", seccompAllowlist, err)
		}
	case Deny:
		if exitCode != 10 {
			c.Fatalf("unexpected exit code for %q %q (%v != %v)", seccompAllowlist, bpfInput, exitCode, 10)
		}
		if err == nil {
			c.Fatalf("unexpected success for %q %q (ran but should have failed)", seccompAllowlist, bpfInput)
		}
	case DenyExplicit:
		if exitCode != 20 {
			c.Fatalf("unexpected exit code for %q %q (%v != %v)", seccompAllowlist, bpfInput, exitCode, 20)
		}
		if err == nil {
			c.Fatalf("unexpected success for %q %q (ran but should have failed)", seccompAllowlist, bpfInput)
		}
	default:
		c.Fatalf("unknown expected result %v", expected)
	}
}

func (s *snapSeccompSuite) TestUnrestricted(c *C) {
	inp := "@unrestricted\n"
	outPath := filepath.Join(c.MkDir(), "bpf")
	err := main.Compile([]byte(inp), outPath)
	c.Assert(err, IsNil)

	expected := [128]byte{'S', 'C', 0x1, 0x1}
	fileContent, err := os.ReadFile(outPath)
	c.Assert(err, IsNil)
	c.Check(fileContent, DeepEquals, expected[:])
}

// TestCompile iterates over a range of textual seccomp allowlist rules and
// mocked kernel syscall input. For each rule, the test consists of compiling
// the rule into a bpf program and then running that program on a virtual bpf
// machine and comparing the bpf machine output to the specified expected
// output and seccomp operation. Eg:
//
//	{"<rule>", "<mocked kernel input>", <seccomp result>}
//
// Eg to test that the rule 'read >=2' is allowed with 'read(2)' and 'read(3)'
// and denied with 'read(1)' and 'read(0)', add the following tests:
//
//	{"read >=2", "read;native;2", Allow},
//	{"read >=2", "read;native;3", Allow},
//	{"read >=2", "read;native;1", main.SeccompRetKill},
//	{"read >=2", "read;native;0", main.SeccompRetKill},
func (s *snapSeccompSuite) TestCompile(c *C) {

	for _, t := range []struct {
		seccompAllowlist string
		bpfInput         string
		expected         int
	}{
		// special
		{"@complain", "execve", Allow},

		// trivial allow
		{"read", "read", Allow},
		{"read\nwrite\nexecve\n", "write", Allow},

		// trivial denial (uses write in allow-list to ensure any
		// errors printing is visible)
		{"write", "ioctl", Deny},

		// test argument filtering syntax, we currently support:
		//   >=, <=, !, <, >, |
		// modifiers.

		// reads >= 2 are ok
		{"read >=2", "read;native;2", Allow},
		{"read >=2", "read;native;3", Allow},
		// but not reads < 2, those get killed
		{"read >=2", "read;native;1", Deny},
		{"read >=2", "read;native;0", Deny},

		// reads <= 2 are ok
		{"read <=2", "read;native;0", Allow},
		{"read <=2", "read;native;1", Allow},
		{"read <=2", "read;native;2", Allow},
		// but not reads >2, those get killed
		{"read <=2", "read;native;3", Deny},
		{"read <=2", "read;native;4", Deny},

		// reads that are not 2 are ok
		{"read !2", "read;native;1", Allow},
		{"read !2", "read;native;3", Allow},
		// but not 2, this gets killed
		{"read !2", "read;native;2", Deny},

		// reads > 2 are ok
		{"read >2", "read;native;4", Allow},
		{"read >2", "read;native;3", Allow},
		// but not reads <= 2, those get killed
		{"read >2", "read;native;2", Deny},
		{"read >2", "read;native;1", Deny},

		// reads < 2 are ok
		{"read <2", "read;native;0", Allow},
		{"read <2", "read;native;1", Allow},
		// but not reads >= 2, those get killed
		{"read <2", "read;native;2", Deny},
		{"read <2", "read;native;3", Deny},

		// FIXME: test maskedEqual better
		{"read |1", "read;native;1", Allow},
		{"read |1", "read;native;2", Deny},

		// exact match, reads == 2 are ok
		{"read 2", "read;native;2", Allow},
		// but not those != 2
		{"read 2", "read;native;3", Deny},
		{"read 2", "read;native;1", Deny},

		// test actual syscalls and their expected usage
		{"ioctl - TIOCSTI", "ioctl;native;-,TIOCSTI", Allow},
		{"ioctl - TIOCSTI", "ioctl;native;-,99", Deny},
		{"ioctl - !TIOCSTI", "ioctl;native;-,TIOCSTI", Deny},
		{"ioctl\n~ioctl - TIOCSTI", "ioctl;native;-,TIOCSTI", DenyExplicit},
		// also check we can deny multiple uses of ioctl but still allow
		// others
		{"ioctl\n~ioctl - TIOCSTI\n~ioctl - TIOCLINUX\nioctl - !TIOCSTI", "ioctl;native;-,TIOCSTI", DenyExplicit},
		{"ioctl\n~ioctl - TIOCSTI\n~ioctl - TIOCLINUX\nioctl - !TIOCSTI", "ioctl;native;-,TIOCLINUX", DenyExplicit},
		{"ioctl\n~ioctl - TIOCSTI\n~ioctl - TIOCLINUX\nioctl - !TIOCSTI", "ioctl;native;-,TIOCGWINSZ", Allow},

		// see CVE-2019-7303
		{"ioctl\n~ioctl - 4294967295|TIOCSTI", "ioctl;native;-,TIOCSTI", DenyExplicit},
		{"ioctl\n~ioctl - 4294967295|TIOCLINUX", "ioctl;native;-,TIOCLINUX", DenyExplicit},

		// test_bad_seccomp_filter_args_clone
		{"setns - CLONE_NEWNET", "setns;native;-,99", Deny},
		{"setns - CLONE_NEWNET", "setns;native;-,CLONE_NEWNET", Allow},

		// test_bad_seccomp_filter_args_mknod
		{"mknod - |S_IFIFO", "mknod;native;-,S_IFIFO", Allow},
		{"mknod - |S_IFIFO", "mknod;native;-,99", Deny},

		// test_bad_seccomp_filter_args_prctl
		{"prctl PR_CAP_AMBIENT_RAISE", "prctl;native;PR_CAP_AMBIENT_RAISE", Allow},
		{"prctl PR_CAP_AMBIENT_RAISE", "prctl;native;99", Deny},

		// test_bad_seccomp_filter_args_prio
		{"setpriority PRIO_PROCESS 0 >=0", "setpriority;native;PRIO_PROCESS,0,19", Allow},
		{"setpriority PRIO_PROCESS 0 >=0", "setpriority;native;99", Deny},
		// negative filtering
		{"setpriority\n~setpriority PRIO_PROCESS 0 >=0", "setpriority;native;PRIO_PROCESS,0,10", DenyExplicit},
		// mix negative/positiv filtering
		// allow setprioty >= 5 but explicitly deny >=10
		{"setpriority PRIO_PROCESS 0 >=5\n~setpriority PRIO_PROCESS 0 >=10", "setpriority;native;PRIO_PROCESS,0,2", Deny},
		{"setpriority PRIO_PROCESS 0 >=5\n~setpriority PRIO_PROCESS 0 >=10", "setpriority;native;PRIO_PROCESS,0,5", Allow},
		{"setpriority PRIO_PROCESS 0 >=5\n~setpriority PRIO_PROCESS 0 >=10", "setpriority;native;PRIO_PROCESS,0,10", DenyExplicit},

		// test_bad_seccomp_filter_args_quotactl
		{"quotactl Q_GETQUOTA", "quotactl;native;Q_GETQUOTA", Allow},
		{"quotactl Q_GETQUOTA", "quotactl;native;99", Deny},

		// test_bad_seccomp_filter_args_termios
		{"ioctl - TIOCSTI", "ioctl;native;-,TIOCSTI", Allow},
		{"ioctl - TIOCSTI", "ioctl;native;-,99", Deny},

		// u:root g:root
		{"fchown - u:root g:root", "fchown;native;-,0,0", Allow},
		{"fchown - u:root g:root", "fchown;native;-,99,0", Deny},
		{"chown - u:root g:root", "chown;native;-,0,0", Allow},
		{"chown - u:root g:root", "chown;native;-,99,0", Deny},

		// u:root -1
		{"chown - u:root -1", "chown;native;-,0,-1", Allow},
		{"chown - u:root -1", "chown;native;-,99,-1", Deny},
		{"chown - -1 u:root", "chown;native;-,-1,0", Allow},
		{"chown - -1 u:root", "chown;native;-,99,0", Deny},
		{"chown - -1 -1", "chown;native;-,-1,-1", Allow},
		{"chown - -1 -1", "chown;native;-,99,-1", Deny},
	} {
		s.runBpf(c, t.seccompAllowlist, t.bpfInput, t.expected)
	}
}

// TestCompileSocket runs in a separate tests so that only this part
// can be skipped when "socketcall()" is used instead of "socket()".
//
// Some architectures (i386, s390x) use the "socketcall" syscall instead
// of "socket". This is the case on Ubuntu 14.04, 17.04, 17.10
func (s *snapSeccompSuite) TestCompileSocket(c *C) {
	if release.ReleaseInfo.ID == "ubuntu" && release.ReleaseInfo.VersionID == "14.04" {
		c.Skip("14.04/i386 uses socketcall which cannot be tested here")
	}

	for _, t := range []struct {
		seccompAllowlist string
		bpfInput         string
		expected         int
	}{

		// test_bad_seccomp_filter_args_socket
		{"socket AF_UNIX", "socket;native;AF_UNIX", Allow},
		{"socket AF_UNIX", "socket;native;99", Deny},
		{"socket - SOCK_STREAM", "socket;native;-,SOCK_STREAM", Allow},
		{"socket - SOCK_STREAM", "socket;native;-,99", Deny},
		{"socket AF_CONN", "socket;native;AF_CONN", Allow},
		{"socket AF_CONN", "socket;native;99", Deny},
	} {
		s.runBpf(c, t.seccompAllowlist, t.bpfInput, t.expected)
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
		// 1 bigger than uint32
		{"chown 0 4294967296", `cannot parse line: cannot parse token "4294967296" .*`},
		// 1 smaller than int32
		{"chown - 0 -2147483649", `cannot parse line: cannot parse token "-2147483649" .*`},
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
		{"setuid u:!", `cannot parse line: cannot parse token "u:!" \(line "setuid u:!"\): "!" must be a valid username`},
		{"setuid u:b@d|npu+", `cannot parse line: cannot parse token "u:b@d|npu+" \(line "setuid u:b@d|npu+"\): "b@d|npu+" must be a valid username`},
		{"setuid u:snap|bad", `cannot parse line: cannot parse token "u:snap|bad" \(line "setuid u:snap|bad"\): "snap|bad" must be a valid username`},
		{"setuid U:root", `cannot parse line: cannot parse token "U:root" .*`},
		{"setuid u:nonexistent", `cannot parse line: cannot parse token "u:nonexistent" \(line "setuid u:nonexistent"\): user: unknown user nonexistent`},
		// g:<groupname>
		{"setgid g:", `cannot parse line: cannot parse token "g:" \(line "setgid g:"\): "" must be a valid group name`},
		{"setgid g:!", `cannot parse line: cannot parse token "g:!" \(line "setgid g:!"\): "!" must be a valid group name`},
		{"setgid g:b@d|npu+", `cannot parse line: cannot parse token "g:b@d|npu+" \(line "setgid g:b@d|npu+"\): "b@d|npu+" must be a valid group name`},
		{"setgid g:snap|bad", `cannot parse line: cannot parse token "g:snap|bad" \(line "setgid g:snap|bad"\): "snap|bad" must be a valid group name`},
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
	if release.ReleaseInfo.ID == "ubuntu" && release.ReleaseInfo.VersionID == "14.04" {
		c.Skip("14.04/i386 uses socketcall which cannot be tested here")
	}

	for _, pre := range []string{"AF", "PF"} {
		for _, i := range []string{"UNIX", "LOCAL", "INET", "INET6", "IPX", "NETLINK", "X25", "AX25", "ATMPVC", "APPLETALK", "PACKET", "ALG", "CAN", "BRIDGE", "NETROM", "ROSE", "NETBEUI", "SECURITY", "KEY", "ASH", "ECONET", "SNA", "IRDA", "PPPOX", "WANPIPE", "BLUETOOTH", "RDS", "LLC", "TIPC", "IUCV", "RXRPC", "ISDN", "PHONET", "IEEE802154", "CAIF", "NFC", "VSOCK", "MPLS", "IB"} {
			seccompAllowlist := fmt.Sprintf("socket %s_%s", pre, i)
			bpfInputGood := fmt.Sprintf("socket;native;%s_%s", pre, i)
			bpfInputBad := "socket;native;99999"
			s.runBpf(c, seccompAllowlist, bpfInputGood, Allow)
			s.runBpf(c, seccompAllowlist, bpfInputBad, Deny)

			for _, j := range []string{"SOCK_STREAM", "SOCK_DGRAM", "SOCK_SEQPACKET", "SOCK_RAW", "SOCK_RDM", "SOCK_PACKET"} {
				seccompAllowlist := fmt.Sprintf("socket %s_%s %s", pre, i, j)
				bpfInputGood := fmt.Sprintf("socket;native;%s_%s,%s", pre, i, j)
				bpfInputBad := fmt.Sprintf("socket;native;%s_%s,9999", pre, i)
				s.runBpf(c, seccompAllowlist, bpfInputGood, Allow)
				s.runBpf(c, seccompAllowlist, bpfInputBad, Deny)
			}
		}
	}

	for _, i := range []string{"NETLINK_ROUTE", "NETLINK_USERSOCK", "NETLINK_FIREWALL", "NETLINK_SOCK_DIAG", "NETLINK_NFLOG", "NETLINK_XFRM", "NETLINK_SELINUX", "NETLINK_ISCSI", "NETLINK_AUDIT", "NETLINK_FIB_LOOKUP", "NETLINK_CONNECTOR", "NETLINK_NETFILTER", "NETLINK_IP6_FW", "NETLINK_DNRTMSG", "NETLINK_KOBJECT_UEVENT", "NETLINK_GENERIC", "NETLINK_SCSITRANSPORT", "NETLINK_ECRYPTFS", "NETLINK_RDMA", "NETLINK_CRYPTO", "NETLINK_INET_DIAG"} {
		for _, j := range []string{"AF_NETLINK", "PF_NETLINK"} {
			seccompAllowlist := fmt.Sprintf("socket %s - %s", j, i)
			bpfInputGood := fmt.Sprintf("socket;native;%s,0,%s", j, i)
			bpfInputBad := fmt.Sprintf("socket;native;%s,0,99", j)
			s.runBpf(c, seccompAllowlist, bpfInputGood, Allow)
			s.runBpf(c, seccompAllowlist, bpfInputBad, Deny)
		}
	}
}

// ported from test_restrictions_working_args_quotactl
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsQuotactl(c *C) {
	for _, arg := range []string{"Q_QUOTAON", "Q_QUOTAOFF", "Q_GETQUOTA", "Q_SETQUOTA", "Q_GETINFO", "Q_SETINFO", "Q_GETFMT", "Q_SYNC", "Q_XQUOTAON", "Q_XQUOTAOFF", "Q_XGETQUOTA", "Q_XSETQLIM", "Q_XGETQSTAT", "Q_XQUOTARM"} {
		// good input
		seccompAllowlist := fmt.Sprintf("quotactl %s", arg)
		bpfInputGood := fmt.Sprintf("quotactl;native;%s", arg)
		s.runBpf(c, seccompAllowlist, bpfInputGood, Allow)
		// bad input
		for _, bad := range []string{"quotactl;native;99999", "read;native;"} {
			s.runBpf(c, seccompAllowlist, bad, Deny)
		}
	}
}

// ported from test_restrictions_working_args_prctl
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsPrctl(c *C) {
	for _, arg := range []string{"PR_CAP_AMBIENT", "PR_CAP_AMBIENT_RAISE", "PR_CAP_AMBIENT_LOWER", "PR_CAP_AMBIENT_IS_SET", "PR_CAP_AMBIENT_CLEAR_ALL", "PR_CAPBSET_READ", "PR_CAPBSET_DROP", "PR_SET_CHILD_SUBREAPER", "PR_GET_CHILD_SUBREAPER", "PR_SET_DUMPABLE", "PR_GET_DUMPABLE", "PR_GET_ENDIAN", "PR_SET_FPEMU", "PR_GET_FPEMU", "PR_SET_FPEXC", "PR_GET_FPEXC", "PR_SET_KEEPCAPS", "PR_GET_KEEPCAPS", "PR_MCE_KILL", "PR_MCE_KILL_GET", "PR_SET_MM", "PR_SET_MM_START_CODE", "PR_SET_MM_END_CODE", "PR_SET_MM_START_DATA", "PR_SET_MM_END_DATA", "PR_SET_MM_START_STACK", "PR_SET_MM_START_BRK", "PR_SET_MM_BRK", "PR_SET_MM_ARG_START", "PR_SET_MM_ARG_END", "PR_SET_MM_ENV_START", "PR_SET_MM_ENV_END", "PR_SET_MM_AUXV", "PR_SET_MM_EXE_FILE", "PR_MPX_ENABLE_MANAGEMENT", "PR_MPX_DISABLE_MANAGEMENT", "PR_SET_NAME", "PR_GET_NAME", "PR_SET_NO_NEW_PRIVS", "PR_GET_NO_NEW_PRIVS", "PR_SET_PDEATHSIG", "PR_GET_PDEATHSIG", "PR_SET_PTRACER", "PR_SET_SECCOMP", "PR_GET_SECCOMP", "PR_SET_SECUREBITS", "PR_GET_SECUREBITS", "PR_SET_THP_DISABLE", "PR_TASK_PERF_EVENTS_DISABLE", "PR_TASK_PERF_EVENTS_ENABLE", "PR_GET_THP_DISABLE", "PR_GET_TID_ADDRESS", "PR_SET_TIMERSLACK", "PR_GET_TIMERSLACK", "PR_SET_TIMING", "PR_GET_TIMING", "PR_SET_TSC", "PR_GET_TSC", "PR_SET_UNALIGN", "PR_GET_UNALIGN"} {
		// good input
		seccompAllowlist := fmt.Sprintf("prctl %s", arg)
		bpfInputGood := fmt.Sprintf("prctl;native;%s", arg)
		s.runBpf(c, seccompAllowlist, bpfInputGood, Allow)
		// bad input
		for _, bad := range []string{"prctl;native;99999", "setpriority;native;"} {
			s.runBpf(c, seccompAllowlist, bad, Deny)
		}

		if arg == "PR_CAP_AMBIENT" {
			for _, j := range []string{"PR_CAP_AMBIENT_RAISE", "PR_CAP_AMBIENT_LOWER", "PR_CAP_AMBIENT_IS_SET", "PR_CAP_AMBIENT_CLEAR_ALL"} {
				seccompAllowlist := fmt.Sprintf("prctl %s %s", arg, j)
				bpfInputGood := fmt.Sprintf("prctl;native;%s,%s", arg, j)
				s.runBpf(c, seccompAllowlist, bpfInputGood, Allow)
				for _, bad := range []string{
					fmt.Sprintf("prctl;native;%s,99999", arg),
					"setpriority;native;",
				} {
					s.runBpf(c, seccompAllowlist, bad, Deny)
				}
			}
		}
	}
}

// ported from test_restrictions_working_args_clone
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsClone(c *C) {
	for _, t := range []struct {
		seccompAllowlist string
		bpfInput         string
		expected         int
	}{
		// good input
		{"setns - CLONE_NEWIPC", "setns;native;-,CLONE_NEWIPC", Allow},
		{"setns - CLONE_NEWNET", "setns;native;-,CLONE_NEWNET", Allow},
		{"setns - CLONE_NEWNS", "setns;native;-,CLONE_NEWNS", Allow},
		{"setns - CLONE_NEWPID", "setns;native;-,CLONE_NEWPID", Allow},
		{"setns - CLONE_NEWUSER", "setns;native;-,CLONE_NEWUSER", Allow},
		{"setns - CLONE_NEWUTS", "setns;native;-,CLONE_NEWUTS", Allow},
		// bad input
		{"setns - CLONE_NEWIPC", "setns;native;-,99", Deny},
		{"setns - CLONE_NEWNET", "setns;native;-,99", Deny},
		{"setns - CLONE_NEWNS", "setns;native;-,99", Deny},
		{"setns - CLONE_NEWPID", "setns;native;-,99", Deny},
		{"setns - CLONE_NEWUSER", "setns;native;-,99", Deny},
		{"setns - CLONE_NEWUTS", "setns;native;-,99", Deny},
	} {
		s.runBpf(c, t.seccompAllowlist, t.bpfInput, t.expected)
	}
}

// ported from test_restrictions_working_args_mknod
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsMknod(c *C) {
	for _, t := range []struct {
		seccompAllowlist string
		bpfInput         string
		expected         int
	}{
		// good input
		{"mknod - S_IFREG", "mknod;native;-,S_IFREG", Allow},
		{"mknod - S_IFCHR", "mknod;native;-,S_IFCHR", Allow},
		{"mknod - S_IFBLK", "mknod;native;-,S_IFBLK", Allow},
		{"mknod - S_IFIFO", "mknod;native;-,S_IFIFO", Allow},
		{"mknod - S_IFSOCK", "mknod;native;-,S_IFSOCK", Allow},
		// bad input
		{"mknod - S_IFREG", "mknod;native;-,999", Deny},
		{"mknod - S_IFCHR", "mknod;native;-,999", Deny},
		{"mknod - S_IFBLK", "mknod;native;-,999", Deny},
		{"mknod - S_IFIFO", "mknod;native;-,999", Deny},
		{"mknod - S_IFSOCK", "mknod;native;-,999", Deny},
	} {
		s.runBpf(c, t.seccompAllowlist, t.bpfInput, t.expected)
	}
}

// ported from test_restrictions_working_args_prio
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsPrio(c *C) {
	for _, t := range []struct {
		seccompAllowlist string
		bpfInput         string
		expected         int
	}{
		// good input
		{"setpriority PRIO_PROCESS", "setpriority;native;PRIO_PROCESS", Allow},
		{"setpriority PRIO_PGRP", "setpriority;native;PRIO_PGRP", Allow},
		{"setpriority PRIO_USER", "setpriority;native;PRIO_USER", Allow},
		// bad input
		{"setpriority PRIO_PROCESS", "setpriority;native;99", Deny},
		{"setpriority PRIO_PGRP", "setpriority;native;99", Deny},
		{"setpriority PRIO_USER", "setpriority;native;99", Deny},
	} {
		s.runBpf(c, t.seccompAllowlist, t.bpfInput, t.expected)
	}
}

// ported from test_restrictions_working_args_termios
func (s *snapSeccompSuite) TestRestrictionsWorkingArgsTermios(c *C) {
	for _, t := range []struct {
		seccompAllowlist string
		bpfInput         string
		expected         int
	}{
		// good input
		{"ioctl - TIOCSTI", "ioctl;native;-,TIOCSTI", Allow},
		// bad input
		{"ioctl - TIOCSTI", "quotactl;native;-,99", Deny},
	} {
		s.runBpf(c, t.seccompAllowlist, t.bpfInput, t.expected)
	}
}

func (s *snapSeccompSuite) TestRestrictionsWorkingArgsUidGid(c *C) {
	// while 'root' user usually has uid 0, 'daemon' user uid may vary
	// across distributions, best lookup the uid directly
	daemonUid, err := osutil.FindUid("daemon")

	if err != nil {
		c.Skip("daemon user not available, perhaps we are in a buildroot jail")
	}

	for _, t := range []struct {
		seccompAllowlist string
		bpfInput         string
		expected         int
	}{
		// good input. 'root' is guaranteed to be '0' and 'daemon' uid
		// was determined at runtime
		{"setuid u:root", "setuid;native;0", Allow},
		{"setuid u:daemon", fmt.Sprintf("setuid;native;%v", daemonUid), Allow},
		{"setgid g:root", "setgid;native;0", Allow},
		{"setgid g:daemon", fmt.Sprintf("setgid;native;%v", daemonUid), Allow},
		// bad input
		{"setuid u:root", "setuid;native;99", Deny},
		{"setuid u:daemon", "setuid;native;99", Deny},
		{"setgid g:root", "setgid;native;99", Deny},
		{"setgid g:daemon", "setgid;native;99", Deny},
	} {
		s.runBpf(c, t.seccompAllowlist, t.bpfInput, t.expected)
	}
}

func (s *snapSeccompSuite) TestCompatArchWorks(c *C) {
	if !s.canCheckCompatArch {
		c.Skip("multi-lib syscall runner not supported by this host")
	}
	for _, t := range []struct {
		arch             string
		seccompAllowlist string
		bpfInput         string
		expected         int
	}{
		// on amd64 we add compat i386
		{"amd64", "read", "read;i386", Allow},
		{"amd64", "read", "read;amd64", Allow},
		{"amd64", "chown - 0 -1", "chown;i386;-,0,-1", Allow},
		{"amd64", "chown - 0 -1", "chown;amd64;-,0,-1", Allow},
		{"amd64", "chown - 0 -1", "chown;i386;-,99,-1", Deny},
		{"amd64", "chown - 0 -1", "chown;amd64;-,99,-1", Deny},
		{"amd64", "setresuid -1 -1 -1", "setresuid;i386;-1,-1,-1", Allow},
		{"amd64", "setresuid -1 -1 -1", "setresuid;amd64;-1,-1,-1", Allow},
		{"amd64", "setresuid -1 -1 -1", "setresuid;i386;-1,99,-1", Deny},
		{"amd64", "setresuid -1 -1 -1", "setresuid;amd64;-1,99,-1", Deny},
	} {
		// It is tricky to mock the architecture here because
		// seccomp is always adding the native arch to the seccomp
		// filter and it will silently discard arches that have
		// an endian mismatch:
		// https://github.com/seccomp/libseccomp/issues/86
		//
		// This means we can not just
		//    main.MockArchDpkgArchitecture(t.arch)
		// here because on endian mismatch the arch will *not* be
		// added
		if arch.DpkgArchitecture() == t.arch {
			s.runBpf(c, t.seccompAllowlist, t.bpfInput, t.expected)
		}
	}
}

func (s *snapSeccompSuite) TestExportBpfErrors(c *C) {
	fout, err := os.Create(filepath.Join(c.MkDir(), "filter"))
	c.Assert(err, IsNil)

	// invalid filter
	_, err = main.ExportBPF(fout, &seccomp.ScmpFilter{})
	c.Check(err, ErrorMatches, "cannot export bpf filter: filter is invalid or uninitialized")
}
