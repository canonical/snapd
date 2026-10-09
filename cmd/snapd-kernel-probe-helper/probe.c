/*
 * Copyright (C) 2026 Canonical Ltd
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

/*
 * probe: raw-syscall AppArmor network mediation probe.
 *
 * Exercises the IPv4 TCP socket lifecycle entirely over loopback,
 * using raw syscalls (no libc linkage):
 *
 *   server: socket -> bind -> listen -> accept -> recv -> send -> recv
 *   client: socket -> connect -> write -> read -> shutdown
 *
 * How it works:
 *
 *   1. Before forking, the process creates an eventfd that both ends
 *      will share. This is the rendezvous channel: it carries the bound
 *      port from server to client and orders the two ends so the client
 *      cannot connect before the server is listening.
 *
 *   2. clone(SIGCHLD) splits the process. The parent runs the server,
 *      the child runs the client. clone is used (not fork) because
 *      asm-generic architectures such as arm64 and riscv64 provide no
 *      SYS_fork; clone(SIGCHLD) is fork-equivalent here and exists on
 *      every architecture Ubuntu ships.
 *
 *   3. The server binds to loopback port 0, letting the kernel assign an
 *      ephemeral port. It recovers that port with getsockname(), calls
 *      listen(), then writes the port number into the eventfd. Using a
 *      kernel-assigned port means the probe never assumes a fixed port
 *      is free and can run concurrently with anything else on the host.
 *
 *   4. The client blocks reading the eventfd, learns the port, and
 *      connect()s to it. Because the write only happens after listen(),
 *      the connect deterministically cannot hit connect-before-listen
 *      (spurious ECONNREFUSED).
 *
 *   5. Server and client then exchange a short message. The client
 *      deliberately uses write()/read() on the connected socket fd --
 *      the exact path TLS stacks (Python ssl, Chromium) use -- because
 *      that is the operation mis-mediated as a file operation by the
 *      AppArmor "file_perm class=net" bug this probe detects.
 *
 *   6. The client initiates shutdown so its ephemeral port takes the
 *      TIME_WAIT, keeping the server's port rebindable. The parent then
 *      reports the child's result.
 *
 * Each end reports the errno of its first failed operation via its exit
 * status so the failing stage can be identified:
 *
 *   0            success (full loopback exchange worked)
 *   1..254       operation failed; value = errno of that operation
 *                (AppArmor mediation denies with EACCES=13)
 *   125          internal error (e.g. clone/eventfd failed)
 *
 * The operation that failed is written to stderr as a single line
 * "FAIL <op> <errno>" by whichever end hits it. On success nothing is
 * printed.
 *
 * Only system headers are used, and only for constants/structs; the
 * binary is fully static and freestanding (-nostdlib).
 */

#include <errno.h>
#include <netinet/in.h>
#include <signal.h>
#include <sys/socket.h>
#include <sys/syscall.h>
#include <sys/wait.h>
#include <unistd.h>

/* Bound every blocking operation so the probe can never hang. */
#define TIMEOUT_SEC 5

/* ---- raw syscall wrapper ---------------------------------------------
 *
 * One generic entry point, raw_syscall(nr, a1..a6), with a per-CPU
 * backend selected at compile time. Covers every architecture Ubuntu
 * ships or is landing: x86_64, i386, arm64, armhf, armel, riscv64,
 * s390x, ppc64el, and 32-bit powerpc (Debian's powerpc port), plus
 * loongarch64 for completeness.
 *
 * The calling conventions below follow the kernel's own nolibc
 * (tools/include/nolibc/arch-*.h) and musl.
 */

#if defined(__x86_64__)

static long raw_syscall(long nr, long a1, long a2, long a3, long a4, long a5, long a6) {
    long ret;
    register long r4 asm("r10") = a4;
    register long r5 asm("r8") = a5;
    register long r6 asm("r9") = a6;

    asm volatile("syscall"
                 : "=a"(ret)
                 : "a"(nr), "D"(a1), "S"(a2), "d"(a3), "r"(r4), "r"(r5), "r"(r6)
                 : "rcx", "r11", "memory");
    return ret;
}

#elif defined(__i386__)

static long raw_syscall(long nr, long a1, long a2, long a3, long a4, long a5, long a6) {
    long ret = nr; /* loaded into eax below */

    /*
     * Args go in ebx, ecx, edx, esi, edi, ebp. The sixth arg needs ebp,
     * which the compiler may be using as the frame pointer, so arg6 is
     * passed through memory and ebp is saved/loaded around the syscall
     * (this is exactly the kernel nolibc arch-x86.h sequence).
     */
    asm volatile(
        "pushl %1\n\t"
        "pushl %%ebp\n\t"
        "movl  4(%%esp), %%ebp\n\t"
        "int   $0x80\n\t"
        "popl  %%ebp\n\t"
        "addl  $4, %%esp"
        : "+a"(ret)
        : "m"(a6), "b"(a1), "c"(a2), "d"(a3), "S"(a4), "D"(a5)
        : "memory", "cc");
    return ret;
}

#elif defined(__aarch64__)

static long raw_syscall(long nr, long a1, long a2, long a3, long a4, long a5, long a6) {
    register long x0 asm("x0") = a1;
    register long x1 asm("x1") = a2;
    register long x2 asm("x2") = a3;
    register long x3 asm("x3") = a4;
    register long x4 asm("x4") = a5;
    register long x5 asm("x5") = a6;
    register long x8 asm("x8") = nr;

    asm volatile("svc #0" : "+r"(x0) : "r"(x1), "r"(x2), "r"(x3), "r"(x4), "r"(x5), "r"(x8) : "memory");
    return x0;
}

#elif defined(__arm__)

static long raw_syscall(long nr, long a1, long a2, long a3, long a4, long a5, long a6) {
    register long r0 asm("r0") = a1;
    register long r1 asm("r1") = a2;
    register long r2 asm("r2") = a3;
    register long r3 asm("r3") = a4;
    register long r4 asm("r4") = a5;
    register long r5 asm("r5") = a6;

    /*
     * In Thumb mode r7 is the frame pointer, so pass the syscall number
     * via a plain input and move it into r7 inside the asm (with
     * save/restore); in ARM mode r7 can be bound directly.
     */
#ifdef __thumb__
    asm volatile(
        "push {r7}\n\t"
        "mov  r7, %6\n\t"
        "svc  0\n\t"
        "pop  {r7}"
        : "+r"(r0)
        : "r"(r1), "r"(r2), "r"(r3), "r"(r4), "r"(r5), "r"(nr)
        : "memory");
#else
    register long r7 asm("r7") = nr;

    asm volatile("svc 0" : "+r"(r0) : "r"(r1), "r"(r2), "r"(r3), "r"(r4), "r"(r5), "r"(r7) : "memory");
#endif
    return r0;
}

#elif defined(__riscv) && __riscv_xlen == 64

static long raw_syscall(long nr, long a1, long a2, long a3, long a4, long a5, long a6) {
    register long a0 asm("a0") = a1;
    register long a1_ asm("a1") = a2;
    register long a2_ asm("a2") = a3;
    register long a3_ asm("a3") = a4;
    register long a4_ asm("a4") = a5;
    register long a5_ asm("a5") = a6;
    register long a7 asm("a7") = nr;

    asm volatile("ecall" : "+r"(a0) : "r"(a1_), "r"(a2_), "r"(a3_), "r"(a4_), "r"(a5_), "r"(a7) : "memory");
    return a0;
}

#elif defined(__loongarch__)

static long raw_syscall(long nr, long a1, long a2, long a3, long a4, long a5, long a6) {
    register long a0 asm("a0") = a1;
    register long a1_ asm("a1") = a2;
    register long a2_ asm("a2") = a3;
    register long a3_ asm("a3") = a4;
    register long a4_ asm("a4") = a5;
    register long a5_ asm("a5") = a6;
    register long a7 asm("a7") = nr;

    asm volatile("syscall 0" : "+r"(a0) : "r"(a1_), "r"(a2_), "r"(a3_), "r"(a4_), "r"(a5_), "r"(a7) : "memory");
    return a0;
}

#elif defined(__s390x__)

static long raw_syscall(long nr, long a1, long a2, long a3, long a4, long a5, long a6) {
    register long r1 asm("r1") = nr;
    register long r2 asm("r2") = a1;
    register long r3 asm("r3") = a2;
    register long r4 asm("r4") = a3;
    register long r5 asm("r5") = a4;
    register long r6 asm("r6") = a5;
    register long r7 asm("r7") = a6;

    asm volatile("svc 0" : "+r"(r2) : "r"(r1), "r"(r3), "r"(r4), "r"(r5), "r"(r6), "r"(r7) : "memory");
    return r2;
}

#elif defined(__powerpc64__) || defined(__powerpc__)

static long raw_syscall(long nr, long a1, long a2, long a3, long a4, long a5, long a6) {
    register long r0 asm("r0") = nr;
    register long r3 asm("r3") = a1;
    register long r4 asm("r4") = a2;
    register long r5 asm("r5") = a3;
    register long r6 asm("r6") = a4;
    register long r7 asm("r7") = a5;
    register long r8 asm("r8") = a6;

    /*
     * PowerPC reports errors via the CR0 Summary-Overflow bit instead of
     * a negative return: on error r3 holds the positive errno and cr0.SO
     * is set. Branch on SO directly (bns+ = branch if SO clear) and
     * negate r3 on the error path, normalising to the kernel-style
     * negative-errno convention the rest of the probe expects. This is
     * the exact sequence the kernel's nolibc uses; testing a hard-coded
     * mfcr bit mask is endian/field-layout fragile, so it is avoided.
     *
     * The 32-bit and 64-bit ABIs share this convention: same instruction
     * (sc), same argument registers (r3..r8), same CR0.SO error report;
     * only the register width differs, which the register asm variables
     * already abstract away.
     */
    asm volatile(
        "sc\n\t"
        "bns+ 1f\n\t"
        "neg  %0, %0\n"
        "1:"
        : "+r"(r3), "+r"(r0)
        : "r"(r4), "r"(r5), "r"(r6), "r"(r7), "r"(r8)
        : "memory", "cr0", "r9", "r10", "r11", "r12");
    return r3;
}

#else
#error "probe: unsupported CPU architecture (need syscall backend)"
#endif

static inline long sys0(long nr) { return raw_syscall(nr, 0, 0, 0, 0, 0, 0); }

static inline long sys1(long nr, long a1) { return raw_syscall(nr, a1, 0, 0, 0, 0, 0); }

static inline long sys2(long nr, long a1, long a2) { return raw_syscall(nr, a1, a2, 0, 0, 0, 0); }

static inline long sys3(long nr, long a1, long a2, long a3) { return raw_syscall(nr, a1, a2, a3, 0, 0, 0); }

static inline long sys4(long nr, long a1, long a2, long a3, long a4) { return raw_syscall(nr, a1, a2, a3, a4, 0, 0); }

static inline long sys5(long nr, long a1, long a2, long a3, long a4, long a5) {
    return raw_syscall(nr, a1, a2, a3, a4, a5, 0);
}

static inline long sys6(long nr, long a1, long a2, long a3, long a4, long a5, long a6) {
    return raw_syscall(nr, a1, a2, a3, a4, a5, a6);
}

/* ---- tiny helpers ---------------------------------------------------- */

/*
 * Freestanding host-to-network byte order conversion. glibc's htons/htonl/
 * ntohs only come as inline definitions when __USE_EXTERN_INLINES is on,
 * which -ffreestanding disables, so under -nostdlib they would be undefined
 * references to libc. Use the compiler builtins instead; they compile to a
 * single bswap/rev instruction (or nothing on big-endian).
 */
#if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
static inline unsigned short probe_htons(unsigned short v) { return __builtin_bswap16(v); }
static inline unsigned long probe_htonl(unsigned long v) { return __builtin_bswap32(v); }
static inline unsigned short probe_ntohs(unsigned short v) { return __builtin_bswap16(v); }
#else
static inline unsigned short probe_htons(unsigned short v) { return v; }
static inline unsigned long probe_htonl(unsigned long v) { return v; }
static inline unsigned short probe_ntohs(unsigned short v) { return v; }
#endif

static void put_str(const char *s) {
    long n = 0;

    while (s[n]) n++;
    sys3(SYS_write, 2, (long)s, n);
}

static void put_dec(long v) {
    char buf[24];
    int i = 0;

    if (v == 0) {
        sys3(SYS_write, 2, (long)"0", 1);
        return;
    }
    if (v < 0) {
        sys3(SYS_write, 2, (long)"-", 1);
        v = -v;
    }
    while (v > 0) {
        buf[i++] = (char)('0' + (v % 10));
        v /= 10;
    }
    while (i > 0) sys3(SYS_write, 2, (long)&buf[--i], 1);
}

/* Report a failed operation and exit with its errno as the status. */
static void fail(const char *op, long err) {
    long code = err < 0 ? -err : err;

    put_str("FAIL ");
    put_str(op);
    put_str(" ");
    put_dec(code);
    put_str("\n");
    sys1(SYS_exit_group, code & 0xff);
    __builtin_unreachable();
}

/*
 * Watchdog: if any blocking syscall (accept/recv/connect) stalls past
 * TIMEOUT_SEC, the default SIGALRM action terminates the process. This
 * guarantees the probe always exits and never hangs the test harness.
 * A terminating SIGALRM yields exit status 128+SIGALRM; the harness
 * only checks for zero/non-zero plus the dmesg audit log, so a hang is
 * reported as a failure rather than a stuck test.
 *
 * setitimer(ITIMER_REAL) is used instead of alarm() because the latter
 * is a legacy syscall that asm-generic architectures (arm64, riscv64)
 * do not provide; setitimer exists everywhere and delivers SIGALRM
 * identically.
 *
 * The struct passed to the raw SYS_setitimer syscall must use the
 * kernel's legacy layout, with long-sized fields: on 32-bit time64
 * systems (current Debian armel/armhf) libc's struct itimerval has
 * 64-bit time_t fields, but the kernel still decodes the legacy layout,
 * so the libc struct would be read as a zero timeout and blocking
 * accept/recv operations could hang indefinitely. Define a private
 * kernel-layout struct instead of using <sys/time.h>'s.
 *
 * The itimerval lives in .data (not on the stack): with no libc, _start
 * has no ABI-guaranteed stack alignment, and gcc may otherwise fill the
 * struct with aligned SSE stores (movaps) that fault on a misaligned
 * stack.
 */
struct kernel_timeval {
    long tv_sec;  /* __kernel_old_time_t */
    long tv_usec; /* __kernel_suseconds_t */
};

struct kernel_itimerval {
    struct kernel_timeval it_interval;
    struct kernel_timeval it_value;
};

/* ITIMER_REAL is 0 on every Linux port; avoid pulling in <sys/time.h>. */
#define KERNEL_ITIMER_REAL 0

static struct kernel_itimerval watchdog_it = {
    .it_value = {.tv_sec = TIMEOUT_SEC, .tv_usec = 0},
    .it_interval = {.tv_sec = 0, .tv_usec = 0},
};

static void arm_watchdog(void) { sys3(SYS_setitimer, KERNEL_ITIMER_REAL, (long)&watchdog_it, 0); }

/* ---- one end of the connection -------------------------------------- */

/*
 * Run the server (is_server != 0) or client (is_server == 0) end.
 * The server binds an ephemeral (kernel-assigned) loopback port, learns
 * it via getsockname(), and hands it to the client through the ready_fd
 * eventfd so no fixed port is ever assumed. The client reads that port
 * from ready_fd before connect()ing, which also removes the
 * connect-before-listen race. On success it returns; on error it exits
 * via fail() and never returns.
 */
static void run_end(int is_server, long ready_fd) {
    struct sockaddr_in addr = {
        .sin_family = AF_INET,
        .sin_port = probe_htons(0),                   /* 0 = kernel assigns an ephemeral port */
        .sin_addr.s_addr = probe_htonl(0x7f000001UL), /* 127.0.0.1 */
    };
    const char msg[] = "ping";
    char buf[16];
    long fd, conn, ret;

    fd = sys3(SYS_socket, AF_INET, SOCK_STREAM, 0);
    if (fd < 0) fail("socket", fd);

    if (is_server) {
        int one = 1;
        unsigned short real_port;

        /* Avoid EADDRINUSE from a previous run's TIME_WAIT socket. */
        sys5(SYS_setsockopt, fd, SOL_SOCKET, SO_REUSEADDR, (long)&one, (long)sizeof(one));
        ret = sys3(SYS_bind, fd, (long)&addr, sizeof(addr));
        if (ret < 0) fail("bind", ret);
        /*
         * Recover the ephemeral port the kernel assigned so it can be
         * passed to the client. getsockname is a plain query (not
         * network I/O), so it is outside the AppArmor send/recv
         * mediation this probe exercises.
         */
        {
            struct sockaddr_in bound;
            socklen_t blen = sizeof(bound);

            ret = sys3(SYS_getsockname, fd, (long)&bound, (long)&blen);
            if (ret < 0) fail("getsockname", ret);
            real_port = probe_ntohs(bound.sin_port);
        }
        ret = sys2(SYS_listen, fd, 4);
        if (ret < 0) fail("listen", ret);
        /*
         * Tell the client we are listening and on which port, so its
         * connect() cannot lose the race against socket()+bind()+listen()
         * (which would surface as a spurious ECONNREFUSED rather than the
         * AppArmor denial this probe is looking for). The eventfd counter
         * is a uint64; the bound port is a nonzero value that fits easily.
         */
        {
            unsigned long long ready = real_port;

            ret = sys3(SYS_write, ready_fd, (long)&ready, 8);
            if (ret < 0) fail("signal", ret);
        }
        /*
         * Block until the child connects. accept4(..., 0) is used rather
         * than accept() because s390x and i386 only provide the former
         * (their accept lives behind the old socketcall multiplexor);
         * accept4 exists on every arch and flags=0 is plain accept.
         */
        conn = sys4(SYS_accept4, fd, 0, 0, 0);
        if (conn < 0) fail("accept", conn);
        ret = sys6(SYS_recvfrom, conn, (long)buf, sizeof(buf), 0, 0, 0);
        if (ret < 0) fail("recv", ret);
        ret = sys6(SYS_sendto, conn, (long)msg, sizeof(msg) - 1, 0, 0, 0);
        if (ret < 0) fail("send", ret);
        /*
         * Let the client initiate shutdown so its ephemeral port takes
         * the TIME_WAIT, keeping this well-known port rebindable.
         * Read until EOF (the client's shutdown) to exercise recv again.
         */
        ret = sys6(SYS_recvfrom, conn, (long)buf, sizeof(buf), 0, 0, 0);
        if (ret < 0) fail("recv", ret);
    } else {
        /* Wait until the server is listen()ing and learn its port. */
        {
            unsigned long long v;

            ret = sys3(SYS_read, ready_fd, (long)&v, 8);
            if (ret < 0) fail("wait", ret);
            /* 0 is never a valid bound port; guard against corruption. */
            if (v == 0 || v > 65535) fail("wait", EPROTO);
            addr.sin_port = probe_htons((unsigned short)v);
        }
        ret = sys3(SYS_connect, fd, (long)&addr, sizeof(addr));
        if (ret < 0) fail("connect", ret);
        /*
         * The bug: socket file-descriptor I/O (write/read, as opposed to
         * send/recv) is mediated by AppArmor as a FILE operation
         * (operation="file_perm" class="net"). Under a profile compiled by
         * snapd 2.77.1's parser 5.0.2 (abi/5.0) that grants "network inet
         * stream" plus file rules, write() on a connected socket is denied
         * with EACCES on affected kernels. send/recv/sendmsg are NOT
         * affected -- only the read/write path. This is exactly what TLS
         * stacks (Python ssl, Chromium) use, which is why HTTPS broke.
         *
         * A correct kernel treats write() on a socket as the network "send"
         * permission and allows it; the probe then completes.
         */
        ret = sys3(SYS_write, fd, (long)msg, sizeof(msg) - 1);
        if (ret < 0) fail("write", ret);
        ret = sys3(SYS_read, fd, (long)buf, sizeof(buf));
        if (ret < 0) fail("read", ret);
        /* Client initiates the close; its ephemeral port holds TIME_WAIT. */
        ret = sys2(SYS_shutdown, fd, SHUT_RDWR);
        if (ret < 0) fail("shutdown", ret);
    }
}

/* ---- entry point ----------------------------------------------------- */

void _start(void) {
    /*
     * No fixed port: the server binds an ephemeral, kernel-assigned
     * loopback port and passes it to the client over ready_fd, so the
     * probe never assumes any particular port is free and can run
     * concurrently with anything else on the device.
     */
    long pid, ready_fd;
    int status = 0;

    /*
     * Readiness rendezvous between server (parent) and client (child).
     * eventfd2 is available on every architecture and creates an
     * anonymous inode, so it needs no filesystem path and is not subject
     * to path-based AppArmor file mediation under the test profile.
     */
    ready_fd = sys2(SYS_eventfd2, 0, 0);
    if (ready_fd < 0) {
        put_str("FAIL eventfd\n");
        sys1(SYS_exit_group, 125);
        __builtin_unreachable();
    }

    /*
     * clone(SIGCHLD, ...) is fork() with an explicit signal; it exists
     * on every architecture (asm-generic ones like arm64/riscv64 have no
     * SYS_fork), so use it unconditionally for portability.
     *
     * s390x is the odd one out: its clone() puts the child stack pointer
     * first and the flags second (opposite of every other arch), so the
     * two leading arguments are swapped there.
     */
#ifdef __s390x__
    pid = sys5(SYS_clone, 0, SIGCHLD, 0, 0, 0);
#else
    pid = sys5(SYS_clone, SIGCHLD, 0, 0, 0, 0);
#endif
    if (pid < 0) {
        put_str("FAIL clone\n");
        sys1(SYS_exit_group, 125);
        __builtin_unreachable();
    }

    if (pid == 0) {
        /* Child: client. run_end either returns (success) or exits. */
        arm_watchdog();
        run_end(0, ready_fd);

        sys1(SYS_exit_group, 0);
        __builtin_unreachable();
    }

    /* Parent: server, then wait for the child. */
    {
        long w;

        arm_watchdog();
        run_end(1, ready_fd);
        w = sys4(SYS_wait4, pid, (long)&status, 0, 0);
        if (w < 0) fail("wait4", w);

        /*
         * Report the child's exit status: errno of its failed op, or 0.
         * A child that died by a signal (e.g. the SIGALRM watchdog) must
         * not be decoded as an exit status: its status has the signal in
         * the low bits and a zero exit-code byte, which would wrongly
         * report a clean kernel. Treat it as an internal error instead.
         */
        if ((status & 0x7f) != 0) {
            put_str("FAIL wait4-signal\n");
            sys1(SYS_exit_group, 125);
        }
        sys1(SYS_exit_group, (status >> 8) & 0xff);
    }
    __builtin_unreachable();
}
