// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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

package main

//#cgo CFLAGS: -D_FILE_OFFSET_BITS=64
//#cgo pkg-config: libseccomp
//#cgo LDFLAGS:
//
//#include <asm/ioctls.h>
//#include <ctype.h>
//#include <errno.h>
//#include <linux/can.h>
//#include <linux/netlink.h>
//#include <sched.h>
//#include <search.h>
//#include <stdbool.h>
//#include <stdio.h>
//#include <stdlib.h>
//#include <string.h>
//#include <sys/prctl.h>
//#include <sys/quota.h>
//#include <sys/resource.h>
//#include <sys/socket.h>
//#include <sys/stat.h>
//#include <sys/types.h>
//#include <sys/utsname.h>
//#include <termios.h>
//#include <unistd.h>
// //The XFS interface requires a 64 bit file system interface
// //but we don't want to leak this anywhere else if not globally
// //defined.
//#ifndef _FILE_OFFSET_BITS
//#define _FILE_OFFSET_BITS 64
//#include <xfs/xqm.h>
//#undef _FILE_OFFSET_BITS
//#else
//#include <xfs/xqm.h>
//#endif
//#include <seccomp.h>
//#include <linux/sched.h>
//#include <linux/seccomp.h>
//#include <arpa/inet.h>
//
//#ifndef AF_IB
//#define AF_IB 27
//#define PF_IB AF_IB
//#endif				// AF_IB
//
//#ifndef AF_MPLS
//#define AF_MPLS 28
//#define PF_MPLS AF_MPLS
//#endif				// AF_MPLS
//
// // https://github.com/sctplab/usrsctp/blob/master/usrsctplib/usrsctp.h
//#ifndef AF_CONN
//#define AF_CONN 123
//#define PF_CONN AF_CONN
//#endif				// AF_CONN
//
//#ifndef PR_CAP_AMBIENT
//#define PR_CAP_AMBIENT 47
//#define PR_CAP_AMBIENT_IS_SET    1
//#define PR_CAP_AMBIENT_RAISE     2
//#define PR_CAP_AMBIENT_LOWER     3
//#define PR_CAP_AMBIENT_CLEAR_ALL 4
//#endif				// PR_CAP_AMBIENT
//
//#ifndef PR_SET_THP_DISABLE
//#define PR_SET_THP_DISABLE 41
//#endif				// PR_SET_THP_DISABLE
//#ifndef PR_GET_THP_DISABLE
//#define PR_GET_THP_DISABLE 42
//#endif				// PR_GET_THP_DISABLE
//
//#ifndef PR_MPX_ENABLE_MANAGEMENT
//#define PR_MPX_ENABLE_MANAGEMENT 43
//#endif
//
//#ifndef PR_MPX_DISABLE_MANAGEMENT
//#define PR_MPX_DISABLE_MANAGEMENT 44
//#endif
//
// //FIXME: ARCH_BAD is defined as ~0 in libseccomp internally, however
// //       this leads to a build failure on 14.04. the important part
// //       is that its an invalid id for libseccomp.
//
//#define ARCH_BAD 0x7FFFFFFF
//#ifndef SCMP_ARCH_AARCH64
//#define SCMP_ARCH_AARCH64 ARCH_BAD
//#endif
//
//#ifndef SCMP_ARCH_PPC
//#define SCMP_ARCH_PPC ARCH_BAD
//#endif
//
//#ifndef SCMP_ARCH_PPC64LE
//#define SCMP_ARCH_PPC64LE ARCH_BAD
//#endif
//
//#ifndef SCMP_ARCH_PPC64
//#define SCMP_ARCH_PPC64 ARCH_BAD
//#endif
//
//#ifndef SCMP_ARCH_S390X
//#define SCMP_ARCH_S390X ARCH_BAD
//#endif
//
//#ifndef SECCOMP_RET_LOG
//#define SECCOMP_RET_LOG 0x7ffc0000U
//#endif
//
//typedef struct seccomp_data kernel_seccomp_data;
//
//__u32 htot32(__u32 arch, __u32 val)
//{
//	if (arch & __AUDIT_ARCH_LE)
//		return htole32(val);
//	else
//		return htobe32(val);
//}
//
//__u64 htot64(__u32 arch, __u64 val)
//{
//	if (arch & __AUDIT_ARCH_LE)
//		return htole64(val);
//	else
//		return htobe64(val);
//}
//
import "C"

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	// FIXME: we want github.com/seccomp/libseccomp-golang but that
	// will not work with trusty because libseccomp-golang checks
	// for the seccomp version and errors if it find one < 2.2.0
	"github.com/mvo5/libseccomp-golang"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/osutil"
)

// libseccomp maximum per ARG_COUNT_MAX in src/arch.h
const ScArgsMaxlength = 6

var seccompResolver = map[string]uint64{
	// man 2 socket - domain and man 5 apparmor.d. AF_ and PF_ are
	// synonymous in the kernel and can be used interchangeably in
	// policy (ie, if use AF_UNIX, don't need a corresponding PF_UNIX
	// rule). See include/linux/socket.h
	"AF_UNIX":       syscall.AF_UNIX,
	"PF_UNIX":       C.PF_UNIX,
	"AF_LOCAL":      syscall.AF_LOCAL,
	"PF_LOCAL":      C.PF_LOCAL,
	"AF_INET":       syscall.AF_INET,
	"PF_INET":       C.PF_INET,
	"AF_INET6":      syscall.AF_INET6,
	"PF_INET6":      C.PF_INET6,
	"AF_IPX":        syscall.AF_IPX,
	"PF_IPX":        C.PF_IPX,
	"AF_NETLINK":    syscall.AF_NETLINK,
	"PF_NETLINK":    C.PF_NETLINK,
	"AF_X25":        syscall.AF_X25,
	"PF_X25":        C.PF_X25,
	"AF_AX25":       syscall.AF_AX25,
	"PF_AX25":       C.PF_AX25,
	"AF_ATMPVC":     syscall.AF_ATMPVC,
	"PF_ATMPVC":     C.PF_ATMPVC,
	"AF_APPLETALK":  syscall.AF_APPLETALK,
	"PF_APPLETALK":  C.PF_APPLETALK,
	"AF_PACKET":     syscall.AF_PACKET,
	"PF_PACKET":     C.PF_PACKET,
	"AF_ALG":        syscall.AF_ALG,
	"PF_ALG":        C.PF_ALG,
	"AF_BRIDGE":     syscall.AF_BRIDGE,
	"PF_BRIDGE":     C.PF_BRIDGE,
	"AF_NETROM":     syscall.AF_NETROM,
	"PF_NETROM":     C.PF_NETROM,
	"AF_ROSE":       syscall.AF_ROSE,
	"PF_ROSE":       C.PF_ROSE,
	"AF_NETBEUI":    syscall.AF_NETBEUI,
	"PF_NETBEUI":    C.PF_NETBEUI,
	"AF_SECURITY":   syscall.AF_SECURITY,
	"PF_SECURITY":   C.PF_SECURITY,
	"AF_KEY":        syscall.AF_KEY,
	"PF_KEY":        C.PF_KEY,
	"AF_ASH":        syscall.AF_ASH,
	"PF_ASH":        C.PF_ASH,
	"AF_ECONET":     syscall.AF_ECONET,
	"PF_ECONET":     C.PF_ECONET,
	"AF_SNA":        syscall.AF_SNA,
	"PF_SNA":        C.PF_SNA,
	"AF_IRDA":       syscall.AF_IRDA,
	"PF_IRDA":       C.PF_IRDA,
	"AF_PPPOX":      syscall.AF_PPPOX,
	"PF_PPPOX":      C.PF_PPPOX,
	"AF_WANPIPE":    syscall.AF_WANPIPE,
	"PF_WANPIPE":    C.PF_WANPIPE,
	"AF_BLUETOOTH":  syscall.AF_BLUETOOTH,
	"PF_BLUETOOTH":  C.PF_BLUETOOTH,
	"AF_RDS":        syscall.AF_RDS,
	"PF_RDS":        C.PF_RDS,
	"AF_LLC":        syscall.AF_LLC,
	"PF_LLC":        C.PF_LLC,
	"AF_TIPC":       syscall.AF_TIPC,
	"PF_TIPC":       C.PF_TIPC,
	"AF_IUCV":       syscall.AF_IUCV,
	"PF_IUCV":       C.PF_IUCV,
	"AF_RXRPC":      syscall.AF_RXRPC,
	"PF_RXRPC":      C.PF_RXRPC,
	"AF_ISDN":       syscall.AF_ISDN,
	"PF_ISDN":       C.PF_ISDN,
	"AF_PHONET":     syscall.AF_PHONET,
	"PF_PHONET":     C.PF_PHONET,
	"AF_IEEE802154": syscall.AF_IEEE802154,
	"PF_IEEE802154": C.PF_IEEE802154,
	"AF_CAIF":       syscall.AF_CAIF,
	"PF_CAIF":       C.AF_CAIF,
	"AF_NFC":        C.AF_NFC,
	"PF_NFC":        C.PF_NFC,
	"AF_VSOCK":      C.AF_VSOCK,
	"PF_VSOCK":      C.PF_VSOCK,
	// may not be defined in socket.h yet
	"AF_IB":   C.AF_IB, // 27
	"PF_IB":   C.PF_IB,
	"AF_MPLS": C.AF_MPLS, // 28
	"PF_MPLS": C.PF_MPLS,
	"AF_CAN":  syscall.AF_CAN,
	"PF_CAN":  C.PF_CAN,
	"AF_CONN": C.AF_CONN, // 123
	"PF_CONN": C.PF_CONN,

	// man 2 socket - type
	"SOCK_STREAM":    syscall.SOCK_STREAM,
	"SOCK_DGRAM":     syscall.SOCK_DGRAM,
	"SOCK_SEQPACKET": syscall.SOCK_SEQPACKET,
	"SOCK_RAW":       syscall.SOCK_RAW,
	"SOCK_RDM":       syscall.SOCK_RDM,
	"SOCK_PACKET":    syscall.SOCK_PACKET,

	// man 2 prctl
	"PR_CAP_AMBIENT":              C.PR_CAP_AMBIENT,
	"PR_CAP_AMBIENT_RAISE":        C.PR_CAP_AMBIENT_RAISE,
	"PR_CAP_AMBIENT_LOWER":        C.PR_CAP_AMBIENT_LOWER,
	"PR_CAP_AMBIENT_IS_SET":       C.PR_CAP_AMBIENT_IS_SET,
	"PR_CAP_AMBIENT_CLEAR_ALL":    C.PR_CAP_AMBIENT_CLEAR_ALL,
	"PR_CAPBSET_READ":             C.PR_CAPBSET_READ,
	"PR_CAPBSET_DROP":             C.PR_CAPBSET_DROP,
	"PR_SET_CHILD_SUBREAPER":      C.PR_SET_CHILD_SUBREAPER,
	"PR_GET_CHILD_SUBREAPER":      C.PR_GET_CHILD_SUBREAPER,
	"PR_SET_DUMPABLE":             C.PR_SET_DUMPABLE,
	"PR_GET_DUMPABLE":             C.PR_GET_DUMPABLE,
	"PR_SET_ENDIAN":               C.PR_SET_ENDIAN,
	"PR_GET_ENDIAN":               C.PR_GET_ENDIAN,
	"PR_SET_FPEMU":                C.PR_SET_FPEMU,
	"PR_GET_FPEMU":                C.PR_GET_FPEMU,
	"PR_SET_FPEXC":                C.PR_SET_FPEXC,
	"PR_GET_FPEXC":                C.PR_GET_FPEXC,
	"PR_SET_KEEPCAPS":             C.PR_SET_KEEPCAPS,
	"PR_GET_KEEPCAPS":             C.PR_GET_KEEPCAPS,
	"PR_MCE_KILL":                 C.PR_MCE_KILL,
	"PR_MCE_KILL_GET":             C.PR_MCE_KILL_GET,
	"PR_SET_MM":                   C.PR_SET_MM,
	"PR_SET_MM_START_CODE":        C.PR_SET_MM_START_CODE,
	"PR_SET_MM_END_CODE":          C.PR_SET_MM_END_CODE,
	"PR_SET_MM_START_DATA":        C.PR_SET_MM_START_DATA,
	"PR_SET_MM_END_DATA":          C.PR_SET_MM_END_DATA,
	"PR_SET_MM_START_STACK":       C.PR_SET_MM_START_STACK,
	"PR_SET_MM_START_BRK":         C.PR_SET_MM_START_BRK,
	"PR_SET_MM_BRK":               C.PR_SET_MM_BRK,
	"PR_SET_MM_ARG_START":         C.PR_SET_MM_ARG_START,
	"PR_SET_MM_ARG_END":           C.PR_SET_MM_ARG_END,
	"PR_SET_MM_ENV_START":         C.PR_SET_MM_ENV_START,
	"PR_SET_MM_ENV_END":           C.PR_SET_MM_ENV_END,
	"PR_SET_MM_AUXV":              C.PR_SET_MM_AUXV,
	"PR_SET_MM_EXE_FILE":          C.PR_SET_MM_EXE_FILE,
	"PR_MPX_ENABLE_MANAGEMENT":    C.PR_MPX_ENABLE_MANAGEMENT,
	"PR_MPX_DISABLE_MANAGEMENT":   C.PR_MPX_DISABLE_MANAGEMENT,
	"PR_SET_NAME":                 C.PR_SET_NAME,
	"PR_GET_NAME":                 C.PR_GET_NAME,
	"PR_SET_NO_NEW_PRIVS":         C.PR_SET_NO_NEW_PRIVS,
	"PR_GET_NO_NEW_PRIVS":         C.PR_GET_NO_NEW_PRIVS,
	"PR_SET_PDEATHSIG":            C.PR_SET_PDEATHSIG,
	"PR_GET_PDEATHSIG":            C.PR_GET_PDEATHSIG,
	"PR_SET_PTRACER":              C.PR_SET_PTRACER,
	"PR_SET_SECCOMP":              C.PR_SET_SECCOMP,
	"PR_GET_SECCOMP":              C.PR_GET_SECCOMP,
	"PR_SET_SECUREBITS":           C.PR_SET_SECUREBITS,
	"PR_GET_SECUREBITS":           C.PR_GET_SECUREBITS,
	"PR_SET_THP_DISABLE":          C.PR_SET_THP_DISABLE,
	"PR_TASK_PERF_EVENTS_DISABLE": C.PR_TASK_PERF_EVENTS_DISABLE,
	"PR_TASK_PERF_EVENTS_ENABLE":  C.PR_TASK_PERF_EVENTS_ENABLE,
	"PR_GET_THP_DISABLE":          C.PR_GET_THP_DISABLE,
	"PR_GET_TID_ADDRESS":          C.PR_GET_TID_ADDRESS,
	"PR_SET_TIMERSLACK":           C.PR_SET_TIMERSLACK,
	"PR_GET_TIMERSLACK":           C.PR_GET_TIMERSLACK,
	"PR_SET_TIMING":               C.PR_SET_TIMING,
	"PR_GET_TIMING":               C.PR_GET_TIMING,
	"PR_SET_TSC":                  C.PR_SET_TSC,
	"PR_GET_TSC":                  C.PR_GET_TSC,
	"PR_SET_UNALIGN":              C.PR_SET_UNALIGN,
	"PR_GET_UNALIGN":              C.PR_GET_UNALIGN,

	// man 2 getpriority
	"PRIO_PROCESS": syscall.PRIO_PROCESS,
	"PRIO_PGRP":    syscall.PRIO_PGRP,
	"PRIO_USER":    syscall.PRIO_USER,

	// man 2 setns
	"CLONE_NEWIPC":  syscall.CLONE_NEWIPC,
	"CLONE_NEWNET":  syscall.CLONE_NEWNET,
	"CLONE_NEWNS":   syscall.CLONE_NEWNS,
	"CLONE_NEWPID":  syscall.CLONE_NEWPID,
	"CLONE_NEWUSER": syscall.CLONE_NEWUSER,
	"CLONE_NEWUTS":  syscall.CLONE_NEWUTS,

	// man 4 tty_ioctl
	"TIOCSTI": syscall.TIOCSTI,

	// man 2 quotactl (with what Linux supports)
	"Q_SYNC":      C.Q_SYNC,
	"Q_QUOTAON":   C.Q_QUOTAON,
	"Q_QUOTAOFF":  C.Q_QUOTAOFF,
	"Q_GETFMT":    C.Q_GETFMT,
	"Q_GETINFO":   C.Q_GETINFO,
	"Q_SETINFO":   C.Q_SETINFO,
	"Q_GETQUOTA":  C.Q_GETQUOTA,
	"Q_SETQUOTA":  C.Q_SETQUOTA,
	"Q_XQUOTAON":  C.Q_XQUOTAON,
	"Q_XQUOTAOFF": C.Q_XQUOTAOFF,
	"Q_XGETQUOTA": C.Q_XGETQUOTA,
	"Q_XSETQLIM":  C.Q_XSETQLIM,
	"Q_XGETQSTAT": C.Q_XGETQSTAT,
	"Q_XQUOTARM":  C.Q_XQUOTARM,

	// man 2 mknod
	"S_IFREG":  syscall.S_IFREG,
	"S_IFCHR":  syscall.S_IFCHR,
	"S_IFBLK":  syscall.S_IFBLK,
	"S_IFIFO":  syscall.S_IFIFO,
	"S_IFSOCK": syscall.S_IFSOCK,

	// man 7 netlink (uapi/linux/netlink.h)
	"NETLINK_ROUTE":          syscall.NETLINK_ROUTE,
	"NETLINK_USERSOCK":       syscall.NETLINK_USERSOCK,
	"NETLINK_FIREWALL":       syscall.NETLINK_FIREWALL,
	"NETLINK_SOCK_DIAG":      C.NETLINK_SOCK_DIAG,
	"NETLINK_NFLOG":          syscall.NETLINK_NFLOG,
	"NETLINK_XFRM":           syscall.NETLINK_XFRM,
	"NETLINK_SELINUX":        syscall.NETLINK_SELINUX,
	"NETLINK_ISCSI":          syscall.NETLINK_ISCSI,
	"NETLINK_AUDIT":          syscall.NETLINK_AUDIT,
	"NETLINK_FIB_LOOKUP":     syscall.NETLINK_FIB_LOOKUP,
	"NETLINK_CONNECTOR":      syscall.NETLINK_CONNECTOR,
	"NETLINK_NETFILTER":      syscall.NETLINK_NETFILTER,
	"NETLINK_IP6_FW":         syscall.NETLINK_IP6_FW,
	"NETLINK_DNRTMSG":        syscall.NETLINK_DNRTMSG,
	"NETLINK_KOBJECT_UEVENT": syscall.NETLINK_KOBJECT_UEVENT,
	"NETLINK_GENERIC":        syscall.NETLINK_GENERIC,
	"NETLINK_SCSITRANSPORT":  syscall.NETLINK_SCSITRANSPORT,
	"NETLINK_ECRYPTFS":       syscall.NETLINK_ECRYPTFS,
	"NETLINK_RDMA":           C.NETLINK_RDMA,
	"NETLINK_CRYPTO":         C.NETLINK_CRYPTO,
	"NETLINK_INET_DIAG":      C.NETLINK_INET_DIAG, // synonymous with NETLINK_SOCK_DIAG
}

const (
	SeccompRetAllow = C.SECCOMP_RET_ALLOW
	SeccompRetKill  = C.SECCOMP_RET_KILL
	SeccompRetLog   = C.SECCOMP_RET_LOG
)

// UbuntuArchToScmpArch takes a dpkg architecture and converts it to
// the seccomp.ScmpArch as used in the libseccomp-golang library
func UbuntuArchToScmpArch(ubuntuArch string) seccomp.ScmpArch {
	switch ubuntuArch {
	case "amd64":
		return seccomp.ArchAMD64
	case "arm64":
		return seccomp.ArchARM64
	case "armhf":
		return seccomp.ArchARM
	case "i386":
		return seccomp.ArchX86
	case "powerpc":
		return seccomp.ArchPPC
	case "ppc64":
		return seccomp.ArchPPC64
	case "ppc64el":
		return seccomp.ArchPPC64LE
	case "s390x":
		return seccomp.ArchS390X
	}
	panic(fmt.Sprintf("cannot map ubuntu arch %q to a seccomp arch", ubuntuArch))
}

// ScmpArchToSeccompNativeArch takes a seccomp.ScmpArch and converts
// it into the native kernel architecture uint32. This is required for
// the tests to simulate the bpf kernel behaviour.
func ScmpArchToSeccompNativeArch(scmpArch seccomp.ScmpArch) uint32 {
	switch scmpArch {
	case seccomp.ArchAMD64:
		return C.SCMP_ARCH_X86_64
	case seccomp.ArchARM64:
		return C.SCMP_ARCH_AARCH64
	case seccomp.ArchARM:
		return C.SCMP_ARCH_ARM
	case seccomp.ArchPPC64:
		return C.SCMP_ARCH_PPC64
	case seccomp.ArchPPC64LE:
		return C.SCMP_ARCH_PPC64LE
	case seccomp.ArchPPC:
		return C.SCMP_ARCH_PPC
	case seccomp.ArchS390X:
		return C.SCMP_ARCH_S390X
	case seccomp.ArchX86:
		return C.SCMP_ARCH_X86
	}
	panic(fmt.Sprintf("cannot map scmpArch %q to a native seccomp arch", scmpArch))
}

// important for unit testing
type SeccompData C.kernel_seccomp_data

func (sc *SeccompData) SetNr(nr seccomp.ScmpSyscall) {
	sc.nr = C.int(C.htot32(C.__u32(sc.arch), C.__u32(nr)))
}
func (sc *SeccompData) SetArch(arch uint32) {
	sc.arch = C.htot32(C.__u32(arch), C.__u32(arch))
}
func (sc *SeccompData) SetArgs(args [6]uint64) {
	for i := range args {
		sc.args[i] = C.htot64(sc.arch, C.__u64(args[i]))
	}
}

func readNumber(token string) (uint64, error) {
	if value, ok := seccompResolver[token]; ok {
		return value, nil
	}

	// Negative numbers are not supported yet, but when they are,
	// adjust this accordingly
	return strconv.ParseUint(token, 10, 64)
}

func parseLine(line string, secFilter *seccomp.ScmpFilter) error {
	// ignore comments and empty lines
	if strings.HasPrefix(line, "#") || line == "" {
		return nil
	}

	// regular line
	tokens := strings.Fields(line)
	if len(tokens[1:]) > ScArgsMaxlength {
		return fmt.Errorf("too many arguments specified for syscall '%s' in line %q", tokens[0], line)
	}

	// fish out syscall
	secSyscall, err := seccomp.GetSyscallFromName(tokens[0])
	if err != nil {
		// FIXME: use structed error in libseccomp-golang when
		//   https://github.com/seccomp/libseccomp-golang/pull/26
		// gets merged. For now, ignore
		// unknown syscalls
		return nil
	}

	var conds []seccomp.ScmpCondition
	for pos, arg := range tokens[1:] {
		var cmpOp seccomp.ScmpCompareOp
		var value uint64
		var err error

		if arg == "-" { // skip arg
			continue
		}

		if strings.HasPrefix(arg, ">=") {
			cmpOp = seccomp.CompareGreaterEqual
			value, err = readNumber(arg[2:])
		} else if strings.HasPrefix(arg, "<=") {
			cmpOp = seccomp.CompareLessOrEqual
			value, err = readNumber(arg[2:])
		} else if strings.HasPrefix(arg, "!") {
			cmpOp = seccomp.CompareNotEqual
			value, err = readNumber(arg[1:])
		} else if strings.HasPrefix(arg, "<") {
			cmpOp = seccomp.CompareLess
			value, err = readNumber(arg[1:])
		} else if strings.HasPrefix(arg, ">") {
			cmpOp = seccomp.CompareGreater
			value, err = readNumber(arg[1:])
		} else if strings.HasPrefix(arg, "|") {
			cmpOp = seccomp.CompareMaskedEqual
			value, err = readNumber(arg[1:])
		} else if strings.HasPrefix(arg, "u:") {
			cmpOp = seccomp.CompareEqual
			value, err = findUid(arg[2:])
			if err != nil {
				return fmt.Errorf("cannot parse token %q (line %q): %v", arg, line, err)
			}
		} else if strings.HasPrefix(arg, "g:") {
			cmpOp = seccomp.CompareEqual
			value, err = findGid(arg[2:])
			if err != nil {
				return fmt.Errorf("cannot parse token %q (line %q): %v", arg, line, err)
			}
		} else {
			cmpOp = seccomp.CompareEqual
			value, err = readNumber(arg)
		}
		if err != nil {
			return fmt.Errorf("cannot parse token %q (line %q)", arg, line)
		}

		var scmpCond seccomp.ScmpCondition
		if cmpOp == seccomp.CompareMaskedEqual {
			scmpCond, err = seccomp.MakeCondition(uint(pos), cmpOp, value, value)
		} else {
			scmpCond, err = seccomp.MakeCondition(uint(pos), cmpOp, value)
		}
		if err != nil {
			return fmt.Errorf("cannot parse line %q: %s", line, err)
		}
		conds = append(conds, scmpCond)
	}

	// Default to adding a precise match if possible. Otherwise
	// let seccomp figure out the architecture specifics.
	if err = secFilter.AddRuleConditionalExact(secSyscall, seccomp.ActAllow, conds); err != nil {
		err = secFilter.AddRuleConditional(secSyscall, seccomp.ActAllow, conds)
	}

	return err
}

// used to mock in tests
var (
	archUbuntuArchitecture       = arch.UbuntuArchitecture
	archUbuntuKernelArchitecture = arch.UbuntuKernelArchitecture
)

var (
	ubuntuArchitecture       = archUbuntuArchitecture()
	ubuntuKernelArchitecture = archUbuntuKernelArchitecture()
)

// For architectures that support a compat architecture, when the
// kernel and userspace match, add the compat arch, otherwise add
// the kernel arch to support the kernel's arch (eg, 64bit kernels with
// 32bit userspace).
func addSecondaryArches(secFilter *seccomp.ScmpFilter) error {
	// note that all architecture strings are in the dpkg
	// architecture notation
	var compatArch seccomp.ScmpArch

	// common case: kernel and userspace have the same arch. We
	// add a compat architecture for some architectures that
	// support it, e.g. on amd64 kernel and userland, we add
	// compat i386 syscalls.
	if ubuntuArchitecture == ubuntuKernelArchitecture {
		switch archUbuntuArchitecture() {
		case "amd64":
			compatArch = seccomp.ArchX86
		case "arm64":
			compatArch = seccomp.ArchARM
		case "ppc64":
			compatArch = seccomp.ArchPPC
		}
	} else {
		// less common case: kernel and userspace have different archs
		// so add a compat architecture that matches the kernel. E.g.
		// an amd64 kernel with i386 userland needs the amd64 secondary
		// arch added to support specialized snaps that might
		// conditionally call 64bit code when the kernel supports it.
		// Note that in this case snapd requests i386 (or arch 'all')
		// snaps. While unusual from a traditional Linux distribution
		// perspective, certain classes of embedded devices are known
		// to use this configuration.
		compatArch = UbuntuArchToScmpArch(archUbuntuKernelArchitecture())
	}

	if compatArch != seccomp.ArchInvalid {
		return secFilter.AddArch(compatArch)
	}

	return nil
}

var errnoOnDenial int16 = C.EPERM

func preprocess(content []byte) (unrestricted, complain bool) {
	scanner := bufio.NewScanner(bytes.NewBuffer(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "@unrestricted":
			unrestricted = true
		case "@complain":
			complain = true
		}
	}
	return unrestricted, complain
}

func complainAction() seccomp.ScmpAction {
	// XXX: Work around some distributions not having a new enough
	// libseccomp-golang that declares ActLog. Instead, we'll guess at its
	// value by adding one to ActAllow and then verify that the string
	// representation is what we expect for ActLog. The value and string is
	// defined in https://github.com/seccomp/libseccomp-golang/pull/29.
	//
	// Ultimately, the fix for this workaround is to be able to use the
	// GetApi() function created in the PR above. It'll tell us if the
	// kernel, libseccomp, and libseccomp-golang all support ActLog.
	var actLog seccomp.ScmpAction = seccomp.ActAllow + 1

	if actLog.String() == "Action: Log system call" {
		return actLog
	}

	// Because ActLog is functionally ActAllow with logging, if we don't
	// support ActLog, fallback to ActLog.
	return seccomp.ActAllow
}

func compile(content []byte, out string) error {
	var err error
	var secFilter *seccomp.ScmpFilter

	unrestricted, complain := preprocess(content)
	switch {
	case unrestricted:
		return osutil.AtomicWrite(out, bytes.NewBufferString("@unrestricted\n"), 0644, 0)
	case complain:
		var complainAct seccomp.ScmpAction = complainAction()

		secFilter, err = seccomp.NewFilter(complainAct)
		if err != nil {
			if complainAct != seccomp.ActAllow {
				// ActLog is only supported in newer versions
				// of the kernel, libseccomp, and
				// libseccomp-golang. Attempt to fall back to
				// ActAllow before erroring out.
				complainAct = seccomp.ActAllow
				secFilter, err = seccomp.NewFilter(complainAct)
			}
		}

		// Set unrestricted to 'true' to fallback to the pre-ActLog
		// behavior of simply setting the allow filter without adding
		// any rules.
		if complainAct == seccomp.ActAllow {
			unrestricted = true
		}
	default:
		secFilter, err = seccomp.NewFilter(seccomp.ActErrno.SetReturnCode(errnoOnDenial))
	}
	if err != nil {
		return fmt.Errorf("cannot create seccomp filter: %s", err)
	}
	if err := addSecondaryArches(secFilter); err != nil {
		return err
	}

	if !unrestricted {
		scanner := bufio.NewScanner(bytes.NewBuffer(content))
		for scanner.Scan() {
			if err := parseLine(scanner.Text(), secFilter); err != nil {
				return fmt.Errorf("cannot parse line: %s", err)
			}
		}
		if scanner.Err(); err != nil {
			return err
		}
	}

	if osutil.GetenvBool("SNAP_SECCOMP_DEBUG") {
		secFilter.ExportPFC(os.Stdout)
	}

	// write atomically
	fout, err := osutil.NewAtomicFile(out, 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return err
	}
	defer fout.Close()

	if err := secFilter.ExportBPF(fout.File); err != nil {
		return err
	}
	return fout.Commit()
}

// Be very strict so usernames and groups specified in policy are widely
// compatible. From NAME_REGEX in /etc/adduser.conf
var userGroupNamePattern = regexp.MustCompile("^[a-z][-a-z0-9_]*$")

// findUid returns the identifier of the given UNIX user name.
func findUid(username string) (uint64, error) {
	if !userGroupNamePattern.MatchString(username) {
		return 0, fmt.Errorf("%q must be a valid username", username)
	}
	return osutil.FindUid(username)
}

// findGid returns the identifier of the given UNIX group name.
func findGid(group string) (uint64, error) {
	if !userGroupNamePattern.MatchString(group) {
		return 0, fmt.Errorf("%q must be a valid group name", group)
	}
	return osutil.FindGid(group)
}

func showSeccompLibraryVersion() error {
	major, minor, micro := seccomp.GetLibraryVersion()
	fmt.Fprintf(os.Stdout, "%d.%d.%d\n", major, minor, micro)
	return nil
}

func main() {
	var err error
	var content []byte

	if len(os.Args) < 2 {
		fmt.Printf("%s: need a command\n", os.Args[0])
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "compile":
		if len(os.Args) < 4 {
			fmt.Println("compile needs an input and output file")
			os.Exit(1)
		}
		content, err = ioutil.ReadFile(os.Args[2])
		if err != nil {
			break
		}
		err = compile(content, os.Args[3])
	case "library-version":
		err = showSeccompLibraryVersion()
	default:
		err = fmt.Errorf("unsupported argument %q", cmd)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
