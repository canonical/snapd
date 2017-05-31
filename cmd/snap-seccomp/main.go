// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
import "C"

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/seccomp/libseccomp-golang"

	"github.com/snapcore/snapd/arch"
)

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

	// man 2 socket - type
	"SOCK_STREAM":    C.SOCK_STREAM,
	"SOCK_DGRAM":     C.SOCK_DGRAM,
	"SOCK_SEQPACKET": C.SOCK_SEQPACKET,
	"SOCK_RAW":       C.SOCK_RAW,
	"SOCK_RDM":       C.SOCK_RDM,
	"SOCK_PACKET":    C.SOCK_PACKET,

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
	"PRIO_PROCESS": C.PRIO_PROCESS,
	"PRIO_PGRP":    C.PRIO_PGRP,
	"PRIO_USER":    C.PRIO_USER,

	// man 2 setns
	"CLONE_NEWIPC":  C.CLONE_NEWIPC,
	"CLONE_NEWNET":  C.CLONE_NEWNET,
	"CLONE_NEWNS":   C.CLONE_NEWNS,
	"CLONE_NEWPID":  C.CLONE_NEWPID,
	"CLONE_NEWUSER": C.CLONE_NEWUSER,
	"CLONE_NEWUTS":  C.CLONE_NEWUTS,

	// man 4 tty_ioctl
	"TIOCSTI": C.TIOCSTI,

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
	"S_IFREG":  C.S_IFREG,
	"S_IFCHR":  C.S_IFCHR,
	"S_IFBLK":  C.S_IFBLK,
	"S_IFIFO":  C.S_IFIFO,
	"S_IFSOCK": C.S_IFSOCK,

	// man 7 netlink (uapi/linux/netlink.h)
	"NETLINK_ROUTE":          C.NETLINK_ROUTE,
	"NETLINK_USERSOCK":       C.NETLINK_USERSOCK,
	"NETLINK_FIREWALL":       C.NETLINK_FIREWALL,
	"NETLINK_SOCK_DIAG":      C.NETLINK_SOCK_DIAG,
	"NETLINK_NFLOG":          C.NETLINK_NFLOG,
	"NETLINK_XFRM":           C.NETLINK_XFRM,
	"NETLINK_SELINUX":        C.NETLINK_SELINUX,
	"NETLINK_ISCSI":          C.NETLINK_ISCSI,
	"NETLINK_AUDIT":          C.NETLINK_AUDIT,
	"NETLINK_FIB_LOOKUP":     C.NETLINK_FIB_LOOKUP,
	"NETLINK_CONNECTOR":      C.NETLINK_CONNECTOR,
	"NETLINK_NETFILTER":      C.NETLINK_NETFILTER,
	"NETLINK_IP6_FW":         C.NETLINK_IP6_FW,
	"NETLINK_DNRTMSG":        C.NETLINK_DNRTMSG,
	"NETLINK_KOBJECT_UEVENT": C.NETLINK_KOBJECT_UEVENT,
	"NETLINK_GENERIC":        C.NETLINK_GENERIC,
	"NETLINK_SCSITRANSPORT":  C.NETLINK_SCSITRANSPORT,
	"NETLINK_ECRYPTFS":       C.NETLINK_ECRYPTFS,
	"NETLINK_RDMA":           C.NETLINK_RDMA,
	"NETLINK_CRYPTO":         C.NETLINK_CRYPTO,
	"NETLINK_INET_DIAG":      C.NETLINK_INET_DIAG, // synonymous with NETLINK_SOCK_DIAG
}

func readNumber(token string) (uint64, error) {
	if value, ok := seccompResolver[token]; ok {
		return value, nil
	}

	return strconv.ParseUint(token, 10, 64)
}

func parseLine(line string, secFilter *seccomp.ScmpFilter) error {
	// comment
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "#") {
		return nil
	}
	if line == "" {
		return nil
	}

	// special
	switch line {
	case "@unrestricted":
		println("unrestricted")
		return nil
	case "@complain":
		println("complain")
		return nil
	}

	// regular line
	tokens := strings.Fields(line)

	// fish out syscall
	secSyscall, err := seccomp.GetSyscallFromName(tokens[0])
	if err != nil {
		// FIXME: add structed error to libseccomp-golang
		if err.Error() == "could not resolve name to syscall" {
			return nil
		}
		return fmt.Errorf("cannot resolve name: %s", err)
	}

	var conds []seccomp.ScmpCondition
	if err != nil {
		return fmt.Errorf("cannot create new filter: %s", err)
	}

	for pos, arg := range tokens[1:] {
		var cmpOp seccomp.ScmpCompareOp
		var value uint64
		var err error

		if arg == "-" {
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
		} else {
			cmpOp = seccomp.CompareEqual
			value, err = readNumber(arg)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot parse token %q\n", arg)
			continue
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

	// FIXME: why do we need this fallback?
	if err = secFilter.AddRuleConditionalExact(secSyscall, seccomp.ActAllow, conds); err != nil {
		err = secFilter.AddRuleConditional(secSyscall, seccomp.ActAllow, conds)
	}

	return err
}

func compile(in, out string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()

	secFilter, err := seccomp.NewFilter(seccomp.ActKill)
	if err != nil {
		return fmt.Errorf("cannot create seccomp filter: %s", err)
	}

	// FIXME: port arch handling properly
	if arch.UbuntuArchitecture() == "amd64" {
		secFilter.AddArch(seccomp.ArchX86)
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if err := parseLine(scanner.Text(), secFilter); err != nil {
			return fmt.Errorf("cannot parse line: %s", err)
		}
	}
	if scanner.Err(); err != nil {
		return err
	}

	// HACK
	fdebug, _ := os.Create(out + ".debug")
	defer fdebug.Close()
	secFilter.ExportPFC(fdebug)

	fout, err := os.Create(out)
	if err != nil {
		return err
	}
	defer fout.Close()

	return secFilter.ExportBPF(fout)
}

func showVersion() error {
	major, minor, micro := seccomp.GetLibraryVersion()
	fmt.Fprintf(os.Stdout, "seccomp version: %d.%d.%d\n", major, minor, micro)
	return nil
}

func main() {
	var err error

	cmd := os.Args[1]
	switch cmd {
	case "compile":
		err = compile(os.Args[2], os.Args[3])
	case "version":
		err = showVersion()
	default:
		err = fmt.Errorf("unsupported argument %q", cmd)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
