/*
 * Copyright (C) 2015-2017 Canonical Ltd
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
#include "config.h"
#include "seccomp-support.h"

#include <asm/ioctls.h>
#include <ctype.h>
#include <errno.h>
#include <linux/can.h>		// needed for search mappings
#include <linux/netlink.h>
#include <sched.h>
#include <search.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/quota.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/types.h>
#include <sys/utsname.h>
#include <termios.h>
#include <xfs/xqm.h>
#include <unistd.h>

#include <seccomp.h>

#include "../libsnap-confine-private/secure-getenv.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

#define sc_map_add(X) sc_map_add_kvp(#X, X)

// libseccomp maximum per ARG_COUNT_MAX in src/arch.h
#define SC_ARGS_MAXLENGTH	6
#define SC_MAX_LINE_LENGTH	82	// 80 + '\n' + '\0'

enum parse_ret {
	PARSE_INVALID_SYSCALL = -2,
	PARSE_ERROR = -1,
	PARSE_OK = 0,
};

struct preprocess {
	bool unrestricted;
	bool complain;
};

/*
 * arg_cmp contains items of type scmp_arg_cmp (from SCMP_CMP macro) and
 * length is the number of items in arg_cmp that are active such that if
 * length is '3' arg_cmp[0], arg_cmp[1] and arg_cmp[2] are used, when length
 * is '1' only arg_cmp[0] and when length is '0', none are used.
 */
struct seccomp_args {
	int syscall_nr;
	unsigned int length;
	struct scmp_arg_cmp arg_cmp[SC_ARGS_MAXLENGTH];
};

struct sc_map_entry {
	ENTRY *e;
	ENTRY *ep;
	struct sc_map_entry *next;
};

struct sc_map_list {
	struct sc_map_entry *list;
	int count;
};

static char *filter_profile_dir = "/var/lib/snapd/seccomp/profiles/";
static struct hsearch_data sc_map_htab;
static struct sc_map_list sc_map_entries;

/*
 * Setup an hsearch map to map strings in the policy (eg, AF_UNIX) to
 * scmp_datum_t values. Abstract away hsearch implementation behind sc_map_*
 * functions in case we want to swap this out.
 *
 * sc_map_init()		- initialize the hash map via linked list of
 * 				  of entries
 * sc_map_add_kvp(key, value)	- create entry from key/value pair and add to
 * 				  linked list
 * sc_map_search(s)	- if found, return scmp_datum_t for key, else set errno
 * sc_map_destroy()	- destroy the hash map and linked list
 */
static scmp_datum_t sc_map_search(const char *s)
{
	ENTRY e;
	ENTRY *ep = NULL;
	scmp_datum_t val = 0;
	errno = 0;

	e.key = (char *)s;
	if (hsearch_r(e, FIND, &ep, &sc_map_htab) == 0)
		die("hsearch_r failed for %s", s);

	if (ep != NULL) {
		scmp_datum_t *val_p = NULL;
		val_p = ep->data;
		val = *val_p;
	} else
		errno = EINVAL;

	return val;
}

static void sc_map_add_kvp(const char *key, scmp_datum_t value)
{
	struct sc_map_entry *node;
	scmp_datum_t *value_copy;

	node = malloc(sizeof(*node));
	if (node == NULL)
		die("Out of memory creating sc_map_entries");

	node->e = malloc(sizeof(*node->e));
	if (node->e == NULL)
		die("Out of memory creating ENTRY");

	node->e->key = strdup(key);
	if (node->e->key == NULL)
		die("Out of memory creating e->key");

	value_copy = malloc(sizeof(*value_copy));
	if (value_copy == NULL)
		die("Out of memory creating e->data");
	*value_copy = value;
	node->e->data = value_copy;

	node->ep = NULL;
	node->next = NULL;

	if (sc_map_entries.list == NULL) {
		sc_map_entries.count = 1;
		sc_map_entries.list = node;
	} else {
		struct sc_map_entry *p = sc_map_entries.list;
		while (p->next != NULL)
			p = p->next;
		p->next = node;
		sc_map_entries.count++;
	}
}

static void sc_map_init()
{
	// initialize the map linked list
	sc_map_entries.list = NULL;
	sc_map_entries.count = 0;

	// build up the map linked list

	// man 2 socket - domain and man 5 apparmor.d
	sc_map_add(AF_UNIX);
	sc_map_add(PF_UNIX);
	sc_map_add(AF_LOCAL);
	sc_map_add(PF_LOCAL);
	sc_map_add(AF_INET);
	sc_map_add(PF_INET);
	sc_map_add(AF_INET6);
	sc_map_add(PF_INET6);
	sc_map_add(AF_IPX);
	sc_map_add(PF_IPX);
	sc_map_add(AF_NETLINK);
	sc_map_add(PF_NETLINK);
	sc_map_add(AF_X25);
	sc_map_add(PF_X25);
	sc_map_add(AF_AX25);
	sc_map_add(PF_AX25);
	sc_map_add(AF_ATMPVC);
	sc_map_add(PF_ATMPVC);
	sc_map_add(AF_APPLETALK);
	sc_map_add(PF_APPLETALK);
	sc_map_add(AF_PACKET);
	sc_map_add(PF_PACKET);
	sc_map_add(AF_ALG);
	sc_map_add(PF_ALG);
	sc_map_add(AF_BRIDGE);
	sc_map_add(PF_BRIDGE);
	sc_map_add(AF_NETROM);
	sc_map_add(PF_NETROM);
	sc_map_add(AF_ROSE);
	sc_map_add(PF_ROSE);
	sc_map_add(AF_NETBEUI);
	sc_map_add(PF_NETBEUI);
	sc_map_add(AF_SECURITY);
	sc_map_add(PF_SECURITY);
	sc_map_add(AF_KEY);
	sc_map_add(PF_KEY);
	sc_map_add(AF_ASH);
	sc_map_add(PF_ASH);
	sc_map_add(AF_ECONET);
	sc_map_add(PF_ECONET);
	sc_map_add(AF_SNA);
	sc_map_add(PF_SNA);
	sc_map_add(AF_IRDA);
	sc_map_add(PF_IRDA);
	sc_map_add(AF_PPPOX);
	sc_map_add(PF_PPPOX);
	sc_map_add(AF_WANPIPE);
	sc_map_add(PF_WANPIPE);
	sc_map_add(AF_BLUETOOTH);
	sc_map_add(PF_BLUETOOTH);
	sc_map_add(AF_RDS);
	sc_map_add(PF_RDS);
	sc_map_add(AF_LLC);
	sc_map_add(PF_LLC);
	sc_map_add(AF_TIPC);
	sc_map_add(PF_TIPC);
	sc_map_add(AF_IUCV);
	sc_map_add(PF_IUCV);
	sc_map_add(AF_RXRPC);
	sc_map_add(PF_RXRPC);
	sc_map_add(AF_ISDN);
	sc_map_add(PF_ISDN);
	sc_map_add(AF_PHONET);
	sc_map_add(PF_PHONET);
	sc_map_add(AF_IEEE802154);
	sc_map_add(PF_IEEE802154);
	sc_map_add(AF_CAIF);
	sc_map_add(PF_CAIF);
	sc_map_add(AF_NFC);
	sc_map_add(PF_NFC);
	sc_map_add(AF_VSOCK);
	sc_map_add(PF_VSOCK);
	sc_map_add(AF_MPLS);
	sc_map_add(PF_MPLS);
	sc_map_add(AF_IB);
	sc_map_add(PF_IB);
	// linux/can.h
	sc_map_add(AF_CAN);
	sc_map_add(PF_CAN);

	// man 2 socket - type
	sc_map_add(SOCK_STREAM);
	sc_map_add(SOCK_DGRAM);
	sc_map_add(SOCK_SEQPACKET);
	sc_map_add(SOCK_RAW);
	sc_map_add(SOCK_RDM);
	sc_map_add(SOCK_PACKET);

	// man 2 prctl
#ifndef PR_CAP_AMBIENT
#define PR_CAP_AMBIENT 47
#define PR_CAP_AMBIENT_IS_SET    1
#define PR_CAP_AMBIENT_RAISE     2
#define PR_CAP_AMBIENT_LOWER     3
#define PR_CAP_AMBIENT_CLEAR_ALL 4
#endif				// PR_CAP_AMBIENT

	sc_map_add(PR_CAP_AMBIENT);
	sc_map_add(PR_CAP_AMBIENT_RAISE);
	sc_map_add(PR_CAP_AMBIENT_LOWER);
	sc_map_add(PR_CAP_AMBIENT_IS_SET);
	sc_map_add(PR_CAP_AMBIENT_CLEAR_ALL);
	sc_map_add(PR_CAPBSET_READ);
	sc_map_add(PR_CAPBSET_DROP);
	sc_map_add(PR_SET_CHILD_SUBREAPER);
	sc_map_add(PR_GET_CHILD_SUBREAPER);
	sc_map_add(PR_SET_DUMPABLE);
	sc_map_add(PR_GET_DUMPABLE);
	sc_map_add(PR_SET_ENDIAN);
	sc_map_add(PR_GET_ENDIAN);
	sc_map_add(PR_SET_FPEMU);
	sc_map_add(PR_GET_FPEMU);
	sc_map_add(PR_SET_FPEXC);
	sc_map_add(PR_GET_FPEXC);
	sc_map_add(PR_SET_KEEPCAPS);
	sc_map_add(PR_GET_KEEPCAPS);
	sc_map_add(PR_MCE_KILL);
	sc_map_add(PR_MCE_KILL_GET);
	sc_map_add(PR_SET_MM);
	sc_map_add(PR_SET_MM_START_CODE);
	sc_map_add(PR_SET_MM_END_CODE);
	sc_map_add(PR_SET_MM_START_DATA);
	sc_map_add(PR_SET_MM_END_DATA);
	sc_map_add(PR_SET_MM_START_STACK);
	sc_map_add(PR_SET_MM_START_BRK);
	sc_map_add(PR_SET_MM_BRK);
	sc_map_add(PR_SET_MM_ARG_START);
	sc_map_add(PR_SET_MM_ARG_END);
	sc_map_add(PR_SET_MM_ENV_START);
	sc_map_add(PR_SET_MM_ENV_END);
	sc_map_add(PR_SET_MM_AUXV);
	sc_map_add(PR_SET_MM_EXE_FILE);
#ifndef PR_MPX_ENABLE_MANAGEMENT
#define PR_MPX_ENABLE_MANAGEMENT 43
#endif				// PR_MPX_ENABLE_MANAGEMENT
	sc_map_add(PR_MPX_ENABLE_MANAGEMENT);
#ifndef PR_MPX_DISABLE_MANAGEMENT
#define PR_MPX_DISABLE_MANAGEMENT 44
#endif				// PR_MPX_DISABLE_MANAGEMENT
	sc_map_add(PR_MPX_DISABLE_MANAGEMENT);
	sc_map_add(PR_SET_NAME);
	sc_map_add(PR_GET_NAME);
	sc_map_add(PR_SET_NO_NEW_PRIVS);
	sc_map_add(PR_GET_NO_NEW_PRIVS);
	sc_map_add(PR_SET_PDEATHSIG);
	sc_map_add(PR_GET_PDEATHSIG);
	sc_map_add(PR_SET_PTRACER);
	sc_map_add(PR_SET_SECCOMP);
	sc_map_add(PR_GET_SECCOMP);
	sc_map_add(PR_SET_SECUREBITS);
	sc_map_add(PR_GET_SECUREBITS);
#ifndef PR_SET_THP_DISABLE
#define PR_SET_THP_DISABLE 41
#endif				// PR_SET_THP_DISABLE
	sc_map_add(PR_SET_THP_DISABLE);
	sc_map_add(PR_TASK_PERF_EVENTS_DISABLE);
	sc_map_add(PR_TASK_PERF_EVENTS_ENABLE);
#ifndef PR_GET_THP_DISABLE
#define PR_GET_THP_DISABLE 42
#endif				// PR_GET_THP_DISABLE
	sc_map_add(PR_GET_THP_DISABLE);
	sc_map_add(PR_GET_TID_ADDRESS);
	sc_map_add(PR_SET_TIMERSLACK);
	sc_map_add(PR_GET_TIMERSLACK);
	sc_map_add(PR_SET_TIMING);
	sc_map_add(PR_GET_TIMING);
	sc_map_add(PR_SET_TSC);
	sc_map_add(PR_GET_TSC);
	sc_map_add(PR_SET_UNALIGN);
	sc_map_add(PR_GET_UNALIGN);

	// man 2 getpriority
	sc_map_add(PRIO_PROCESS);
	sc_map_add(PRIO_PGRP);
	sc_map_add(PRIO_USER);

	// man 2 setns
	sc_map_add(CLONE_NEWIPC);
	sc_map_add(CLONE_NEWNET);
	sc_map_add(CLONE_NEWNS);
	sc_map_add(CLONE_NEWPID);
	sc_map_add(CLONE_NEWUSER);
	sc_map_add(CLONE_NEWUTS);

	// man 4 tty_ioctl
	sc_map_add(TIOCSTI);

	// man 2 quotactl (with what Linux supports)
	sc_map_add(Q_SYNC);
	sc_map_add(Q_QUOTAON);
	sc_map_add(Q_QUOTAOFF);
	sc_map_add(Q_GETFMT);
	sc_map_add(Q_GETINFO);
	sc_map_add(Q_SETINFO);
	sc_map_add(Q_GETQUOTA);
	sc_map_add(Q_SETQUOTA);
	sc_map_add(Q_XQUOTAON);
	sc_map_add(Q_XQUOTAOFF);
	sc_map_add(Q_XGETQUOTA);
	sc_map_add(Q_XSETQLIM);
	sc_map_add(Q_XGETQSTAT);
	sc_map_add(Q_XQUOTARM);

	// man 7 netlink (uapi/linux/netlink.h)
	sc_map_add(NETLINK_ROUTE);
	sc_map_add(NETLINK_USERSOCK);
	sc_map_add(NETLINK_FIREWALL);
	sc_map_add(NETLINK_SOCK_DIAG);
	sc_map_add(NETLINK_NFLOG);
	sc_map_add(NETLINK_XFRM);
	sc_map_add(NETLINK_SELINUX);
	sc_map_add(NETLINK_ISCSI);
	sc_map_add(NETLINK_AUDIT);
	sc_map_add(NETLINK_FIB_LOOKUP);
	sc_map_add(NETLINK_CONNECTOR);
	sc_map_add(NETLINK_NETFILTER);
	sc_map_add(NETLINK_IP6_FW);
	sc_map_add(NETLINK_DNRTMSG);
	sc_map_add(NETLINK_KOBJECT_UEVENT);
	sc_map_add(NETLINK_GENERIC);
	sc_map_add(NETLINK_SCSITRANSPORT);
	sc_map_add(NETLINK_ECRYPTFS);
	sc_map_add(NETLINK_RDMA);
	sc_map_add(NETLINK_CRYPTO);
	sc_map_add(NETLINK_INET_DIAG);

	// initialize the htab for our map
	memset((void *)&sc_map_htab, 0, sizeof(sc_map_htab));
	if (hcreate_r(sc_map_entries.count, &sc_map_htab) == 0)
		die("could not create map");

	// add elements from linked list to map
	struct sc_map_entry *p = sc_map_entries.list;
	while (p != NULL) {
		errno = 0;
		if (hsearch_r(*p->e, ENTER, &p->ep, &sc_map_htab) == 0)
			die("hsearch_r failed");

		if (&p->ep == NULL)
			die("could not initialize map");

		p = p->next;
	}
}

static void sc_map_destroy()
{
	// this frees all of the nodes' ep so we don't have to below
	hdestroy_r(&sc_map_htab);

	struct sc_map_entry *next = sc_map_entries.list;
	struct sc_map_entry *p = NULL;
	while (next != NULL) {
		p = next;
		next = p->next;
		free(p->e->key);
		free(p->e->data);
		free(p->e);
		free(p);
	}
}

/* Caller must check if errno != 0 */
static scmp_datum_t read_number(const char *s)
{
	scmp_datum_t val = 0;

	errno = 0;

	// per seccomp.h definition of scmp_datum_t, negative numbers are not
	// supported, so fail if we see one or if we get one. Also fail if
	// string is 0 length.
	if (s[0] == '-' || s[0] == '\0') {
		errno = EINVAL;
		return val;
	}
	// check if number
	for (int i = 0; i < strlen(s); i++) {
		if (isdigit(s[i]) == 0) {
			errno = EINVAL;
			break;
		}
	}
	if (errno == 0) {	// found a number, so parse it
		char *end;
		// strtol may set errno to ERANGE
		val = strtoul(s, &end, 10);
		if (end == s || *end != '\0')
			errno = EINVAL;
	} else			// try our map (sc_map_search sets errno)
		val = sc_map_search(s);

	return val;
}

static int parse_line(char *line, struct seccomp_args *sargs)
{
	// strtok_r needs a pointer to keep track of where it is in the
	// string.
	char *buf_saveptr;

	// Initialize our struct
	sargs->length = 0;
	sargs->syscall_nr = -1;

	if (strlen(line) == 0)
		return PARSE_ERROR;

	// Initialize tokenizer and obtain first token.
	char *buf_token = strtok_r(line, " \t", &buf_saveptr);
	if (buf_token == NULL)
		return PARSE_ERROR;

	// syscall not available on this arch/kernel
	sargs->syscall_nr = seccomp_syscall_resolve_name(buf_token);
	if (sargs->syscall_nr == __NR_SCMP_ERROR)
		return PARSE_INVALID_SYSCALL;

	// Parse for syscall arguments. Since we haven't yet searched for the
	// next token, buf_token is still the syscall itself so start 'pos' as
	// -1 and only if there is an arg to parse, increment it.
	int pos = -1;
	while (pos < SC_ARGS_MAXLENGTH) {
		buf_token = strtok_r(NULL, " \t", &buf_saveptr);
		if (buf_token == NULL)
			break;
		// we found a token, so increment position and process it
		pos++;
		if (strcmp(buf_token, "-") == 0)	// skip arg
			continue;

		enum scmp_compare op = -1;
		scmp_datum_t value = 0;
		if (strlen(buf_token) == 0) {
			return PARSE_ERROR;
		} else if (strlen(buf_token) == 1) {
			// syscall N (length of '1' indicates a single digit)
			op = SCMP_CMP_EQ;
			value = read_number(buf_token);
		} else if (strncmp(buf_token, ">=", 2) == 0) {
			// syscall >=N
			op = SCMP_CMP_GE;
			value = read_number(&buf_token[2]);
		} else if (strncmp(buf_token, "<=", 2) == 0) {
			// syscall <=N
			op = SCMP_CMP_LE;
			value = read_number(&buf_token[2]);
		} else if (strncmp(buf_token, "!", 1) == 0) {
			// syscall !N
			op = SCMP_CMP_NE;
			value = read_number(&buf_token[1]);
		} else if (strncmp(buf_token, ">", 1) == 0) {
			// syscall >N
			op = SCMP_CMP_GT;
			value = read_number(&buf_token[1]);
		} else if (strncmp(buf_token, "<", 1) == 0) {
			// syscall <N
			op = SCMP_CMP_LT;
			value = read_number(&buf_token[1]);
		} else {
			// syscall NNN
			op = SCMP_CMP_EQ;
			value = read_number(buf_token);
		}
		if (errno != 0)
			return PARSE_ERROR;

		sargs->arg_cmp[sargs->length] = SCMP_CMP(pos, op, value);
		sargs->length++;

		//printf("\nDEBUG: SCMP_CMP(%d, %d, %llu)\n", pos, op, value);
	}
	// too many args
	if (pos >= SC_ARGS_MAXLENGTH)
		return PARSE_ERROR;

	return PARSE_OK;
}

// strip whitespace from the end of the given string (inplace)
static size_t trim_right(char *s, size_t slen)
{
	while (slen > 0 && isspace(s[slen - 1])) {
		s[--slen] = 0;
	}
	return slen;
}

// Read a relevant line and return the length. Return length '0' for comments,
// empty lines and lines with only whitespace (so a caller can easily skip
// them). The line buffer is right whitespaced trimmed and the final length of
// the trimmed line is returned.
static size_t validate_and_trim_line(char *buf, size_t buf_len, size_t lineno)
{
	size_t len = 0;

	// comment, ignore
	if (buf[0] == '#')
		return len;

	// ensure the entire line was read
	len = strlen(buf);
	if (len == 0)
		return len;
	else if (buf[len - 1] != '\n' && len > (buf_len - 2)) {
		fprintf(stderr,
			"seccomp filter line %zu was too long (%zu characters max)\n",
			lineno, buf_len - 2);
		errno = 0;
		die("aborting");
	}
	// kill final newline
	len = trim_right(buf, len);

	return len;
}

static void preprocess_filter(FILE * f, struct preprocess *p)
{
	char buf[SC_MAX_LINE_LENGTH];
	size_t lineno = 0;

	p->unrestricted = false;
	p->complain = false;

	while (fgets(buf, sizeof(buf), f) != NULL) {
		lineno++;

		// skip policy-irrelevant lines
		if (validate_and_trim_line(buf, sizeof(buf), lineno) == 0)
			continue;

		// check for special "@unrestricted" rule which short-circuits
		// seccomp sandbox
		if (strcmp(buf, "@unrestricted") == 0)
			p->unrestricted = true;

		// check for special "@complain" rule
		if (strcmp(buf, "@complain") == 0)
			p->complain = true;
	}

	if (fseek(f, 0L, SEEK_SET) != 0)
		die("could not rewind file");

	return;
}

static uint32_t uts_machine_to_seccomp_arch(const char *uts_machine)
{
	if (strcmp(uts_machine, "i686") == 0)
		return SCMP_ARCH_X86;
	else if (strcmp(uts_machine, "x86_64") == 0)
		return SCMP_ARCH_X86_64;
	else if (strncmp(uts_machine, "armv7", 5) == 0)
		return SCMP_ARCH_ARM;
#if defined (SCMP_ARCH_AARCH64)
	else if (strncmp(uts_machine, "aarch64", 7) == 0)
		return SCMP_ARCH_AARCH64;
#endif
#if defined (SCMP_ARCH_PPC64LE)
	else if (strncmp(uts_machine, "ppc64le", 7) == 0)
		return SCMP_ARCH_PPC64LE;
#endif
#if defined (SCMP_ARCH_PPC64)
	else if (strncmp(uts_machine, "ppc64", 5) == 0)
		return SCMP_ARCH_PPC64;
#endif
#if defined (SCMP_ARCH_PPC)
	else if (strncmp(uts_machine, "ppc", 3) == 0)
		return SCMP_ARCH_PPC;
#endif
#if defined (SCMP_ARCH_S390X)
	else if (strncmp(uts_machine, "s390x", 5) == 0)
		return SCMP_ARCH_S390X;
#endif
	return 0;
}

static uint32_t get_hostarch(void)
{
	struct utsname uts;
	if (uname(&uts) < 0)
		die("uname() failed");
	uint32_t arch = uts_machine_to_seccomp_arch(uts.machine);
	if (arch > 0)
		return arch;
	// Just return the seccomp userspace native arch if we can't detect the
	// kernel host arch.
	return seccomp_arch_native();
}

static void sc_add_seccomp_archs(scmp_filter_ctx * ctx)
{
	uint32_t native_arch = seccomp_arch_native();	// seccomp userspace
	uint32_t host_arch = get_hostarch();	// kernel
	uint32_t compat_arch = 0;

	debug("host arch (kernel) is '%d'", host_arch);
	debug("native arch (userspace) is '%d'", native_arch);

	// For architectures that support a compat architecture, when the
	// kernel and userspace match, add the compat arch, otherwise add
	// the kernel arch to support the kernel's arch (eg, 64bit kernels with
	// 32bit userspace).
	if (host_arch == native_arch) {
		switch (host_arch) {
#if defined (SCMP_ARCH_X86_64)
		case SCMP_ARCH_X86_64:
			compat_arch = SCMP_ARCH_X86;
			break;
#endif
#if defined(SCMP_ARCH_AARCH64)
		case SCMP_ARCH_AARCH64:
			compat_arch = SCMP_ARCH_ARM;
			break;
#endif
#if defined (SCMP_ARCH_PPC64)
		case SCMP_ARCH_PPC64:
			compat_arch = SCMP_ARCH_PPC;
			break;
#endif
		default:
			break;
		}
	} else
		compat_arch = host_arch;

	if (compat_arch > 0 && seccomp_arch_exist(ctx, compat_arch) == -EEXIST) {
		debug("adding compat arch '%d'", compat_arch);
		if (seccomp_arch_add(ctx, compat_arch) < 0)
			die("seccomp_arch_add(..., compat_arch) failed");
	}
}

scmp_filter_ctx sc_prepare_seccomp_context(const char *filter_profile)
{
	int rc = 0;
	scmp_filter_ctx ctx = NULL;
	FILE *f = NULL;
	size_t lineno = 0;
	uid_t real_uid, effective_uid, saved_uid;
	struct preprocess pre;
	struct seccomp_args sargs;

	debug("preparing seccomp profile associated with security tag %s",
	      filter_profile);

	// initialize hsearch map
	sc_map_init();

	ctx = seccomp_init(SCMP_ACT_KILL);
	if (ctx == NULL) {
		errno = ENOMEM;
		die("seccomp_init() failed");
	}
	// Setup native arch and any compatibility archs
	sc_add_seccomp_archs(ctx);

	// Disable NO_NEW_PRIVS because it interferes with exec transitions in
	// AppArmor. Unfortunately this means that security policies must be
	// very careful to not allow the following otherwise apps can escape
	// the sandbox:
	//   - seccomp syscall
	//   - prctl with PR_SET_SECCOMP
	//   - ptrace (trace) in AppArmor
	//   - capability sys_admin in AppArmor
	// Note that with NO_NEW_PRIVS disabled, CAP_SYS_ADMIN is required to
	// change the seccomp sandbox.

	if (getresuid(&real_uid, &effective_uid, &saved_uid) != 0)
		die("could not find user IDs");

	// If running privileged or capable of raising, disable nnp
	if (real_uid == 0 || effective_uid == 0 || saved_uid == 0)
		if (seccomp_attr_set(ctx, SCMP_FLTATR_CTL_NNP, 0) != 0)
			die("Cannot disable nnp");

	// Note that secure_gettenv will always return NULL when suid, so
	// SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR can't be (ab)used in that case.
	if (secure_getenv("SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR") != NULL)
		filter_profile_dir =
		    secure_getenv("SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR");

	char profile_path[512];	// arbitrary path name limit
	sc_must_snprintf(profile_path, sizeof(profile_path), "%s/%s",
			 filter_profile_dir, filter_profile);

	f = fopen(profile_path, "r");
	if (f == NULL) {
		fprintf(stderr, "Can not open %s (%s)\n", profile_path,
			strerror(errno));
		die("aborting");
	}
	// Note, preprocess_filter() die()s on error
	preprocess_filter(f, &pre);

	if (pre.unrestricted) {
		seccomp_release(ctx);
		ctx = NULL;
		goto out;
	}
	// FIXME: right now complain mode is the equivalent to unrestricted.
	// We'll want to change this once we seccomp logging is in order.
	if (pre.complain) {
		seccomp_release(ctx);
		ctx = NULL;
		goto out;
	}

	char buf[SC_MAX_LINE_LENGTH];
	while (fgets(buf, sizeof(buf), f) != NULL) {
		lineno++;

		// skip policy-irrelevant lines
		if (validate_and_trim_line(buf, sizeof(buf), lineno) == 0)
			continue;

		char *buf_copy = strdup(buf);
		if (buf_copy == NULL)
			die("Out of memory");

		int pr_rc = parse_line(buf_copy, &sargs);
		free(buf_copy);
		if (pr_rc != PARSE_OK) {
			// as this is a syscall whitelist an invalid syscall
			// is ok and the error can be ignored
			if (pr_rc == PARSE_INVALID_SYSCALL)
				continue;
			die("could not parse line");
		}

		rc = seccomp_rule_add_exact_array(ctx, SCMP_ACT_ALLOW,
						  sargs.syscall_nr,
						  sargs.length, sargs.arg_cmp);
		if (rc != 0) {
			rc = seccomp_rule_add_array(ctx, SCMP_ACT_ALLOW,
						    sargs.syscall_nr,
						    sargs.length,
						    sargs.arg_cmp);
			if (rc != 0) {
				fprintf(stderr,
					"seccomp_rule_add_array failed with %i for '%s'\n",
					rc, buf);
				errno = 0;
				die("aborting");
			}
		}
	}

 out:
	if (f != NULL) {
		if (fclose(f) != 0)
			die("could not close seccomp file");
	}
	sc_map_destroy();
	return ctx;
}

void sc_load_seccomp_context(scmp_filter_ctx ctx)
{
	int rc;
	uid_t real_uid, effective_uid, saved_uid;

	// if sc_prepare_seccomp_context() sees @unrestricted or @complain it bails
	// out early and destroys the context object. In that case we have nothing
	// to do.
	if (ctx == NULL) {
		return;
	}

	if (getresuid(&real_uid, &effective_uid, &saved_uid) != 0)
		die("could not find user IDs");

	// If not root but can raise, then raise privileges to load seccomp
	// policy since we don't have nnp
	debug("raising privileges to load seccomp profile");
	if (effective_uid != 0 && saved_uid == 0) {
		if (seteuid(0) != 0)
			die("seteuid failed");
		if (geteuid() != 0)
			die("raising privs before seccomp_load did not work");
	}
	// load it into the kernel
	debug("loading seccomp profile into the kernel");
	rc = seccomp_load(ctx);
	if (rc != 0) {
		fprintf(stderr, "seccomp_load failed with %i\n", rc);
		die("aborting");
	}
	// drop privileges again
	debug("dropping privileges after loading seccomp profile");
	if (geteuid() == 0) {
		unsigned real_uid = getuid();
		if (seteuid(real_uid) != 0)
			die("seteuid failed");
		if (real_uid != 0 && geteuid() == 0)
			die("dropping privs after seccomp_load did not work");
	}
}

void sc_cleanup_seccomp_release(scmp_filter_ctx * ptr)
{
	seccomp_release(*ptr);
}
