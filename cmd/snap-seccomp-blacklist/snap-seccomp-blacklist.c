#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include <seccomp.h>

__attribute__((format(printf, 1, 2))) static void showerr(const char *fmt, ...);

static void showerr(const char *fmt, ...) {
    va_list va;
    va_start(va, fmt);
    vfprintf(stderr, fmt, va);
    fputc('\n', stderr);
    va_end(va);
}

static int populate_filter(scmp_filter_ctx ctx, const uint32_t *arch_tags, size_t num_arch_tags) {
    int sc_err;

    /* If the native architecture is not one of the supported 64bit
     * architectures listed in main in le_arch_tags and be_arch_tags, then
     * remove it.
     *
     * Libseccomp automatically adds the native architecture to each new filter.
     * If the native architecture is a 32bit-one then we will hit a bug in libseccomp
     * and the generated BPF program is incorrect as described below. */
    uint32_t native_arch = seccomp_arch_native();
    bool remove_native_arch = true;
    for (size_t i = 0; i < num_arch_tags; ++i) {
        if (arch_tags[i] == native_arch) {
            remove_native_arch = false;
            break;
        }
    }
    if (remove_native_arch) {
        sc_err = seccomp_arch_remove(ctx, SCMP_ARCH_NATIVE);
        if (sc_err < 0) {
            showerr("cannot remove native architecture");
            return sc_err;
        }
    }

    /* Add 64-bit architectures supported by snapd into the seccomp filter.
     *
     * The documentation of seccomp_arch_add() is confusing. It says that after
     * this call any new rules will be added to this architecture. This is
     * correct. It doesn't, however, explain that the rules will be multiplied
     * and re-written as explained below. */
    for (size_t i = 0; i < num_arch_tags; ++i) {
        uint32_t arch_tag = arch_tags[i];
        sc_err = seccomp_arch_add(ctx, arch_tag);
        if (sc_err < 0 && sc_err != -EEXIST) {
            showerr("cannot add architecture %x", arch_tag);
            return sc_err;
        }
    }

    /* When the rule set doesn't match one of the architectures above then the
     * resulting action should be a "allow" rather than "kill". We don't add
     * any of the 32bit architectures since there is no need for any extra
     * filtering there. */
    sc_err = seccomp_attr_set(ctx, SCMP_FLTATR_ACT_BADARCH, SCMP_ACT_ALLOW);
    if (sc_err < 0) {
        showerr("cannot set action for unknown architectures");
        return sc_err;
    }

    /* Resolve the name of "ioctl" on this architecture. We are not using the
     * system call number as available through the appropriate linux-specific
     * header. This allows us to use a system call number that is not defined
     * for the current architecture. This does not matter here, in this
     * specific program, however it is more generic. In addition this is more
     * in sync with the snap-seccomp program, which does the same for every
     * system call. */
    int sys_ioctl_nr;
    sys_ioctl_nr = seccomp_syscall_resolve_name("ioctl");
    if (sys_ioctl_nr == __NR_SCMP_ERROR) {
        showerr("cannot resolve ioctl system call number");
        return -ESRCH;
    }

    /* All of the rules must be added for the native architecture (using native
     * system call numbers). When the final program is generated the set of
     * architectures added earlier will be used to determine the correct system
     * call number for each architecture.
     *
     * In other words, arguments to scmp_rule_add() must always use native
     * system call numbers. Translation for the correct architecture will be
     * performed internally. This is not documented in libseccomp, but correct
     * operation was confirmed using the pseudo-code program and the bpf_dbg
     * tool from the kernel tools/bpf directory.
     *
     * NOTE: not using scmp_rule_add_exact as that was not doing anything
     * at all (presumably due to having all the architectures defined). */

    struct scmp_arg_cmp no_tty_inject = {
        /* We learned that existing programs make legitimate requests with all
         * bits set in the more significant 32bit word of the 64 bit double
         * word. While this kernel behavior remains suspect and presumably
         * undesired it is unlikely to change for backwards compatibility
         * reasons. As such we cannot block all requests with high-bits set.
         *
         * When faced with ioctl(fd, request); refuse to proceed when
         * request&0xffffffff == TIOCSTI. This specific way to encode the
         * filter has the following important properties:
         *
         * - it blocks ioctl(fd, TIOCSTI, ptr).
         * - it also blocks ioctl(fd, (1UL<<32) | TIOCSTI, ptr).
         * - it doesn't block ioctl(fd, (1UL<<32) | (request not equal to TIOCSTI), ptr); */
        .arg = 1,
        .op = SCMP_CMP_MASKED_EQ,
        .datum_a = 0xffffffffUL,
        .datum_b = TIOCSTI,
    };
    sc_err = seccomp_rule_add(ctx, SCMP_ACT_ERRNO(EPERM), sys_ioctl_nr, 1, no_tty_inject);

    /* also block use of TIOCLINUX */
    no_tty_inject.datum_b = TIOCLINUX;
    sc_err = seccomp_rule_add(ctx, SCMP_ACT_ERRNO(EPERM), sys_ioctl_nr, 1, no_tty_inject);

    if (sc_err < 0) {
        showerr("cannot add rule preventing the use high bits in ioctl");
        return sc_err;
    }
    return 0;
}

typedef struct arch_set {
    const char *name;
    const uint32_t *arch_tags;
    size_t num_arch_tags;
} arch_set;

int main(int argc, char **argv) {
    const uint32_t le_arch_tags[] = {
        SCMP_ARCH_X86_64,
        SCMP_ARCH_AARCH64,
        SCMP_ARCH_PPC64LE,
        SCMP_ARCH_S390X,
    };
    const uint32_t be_arch_tags[] = {
        SCMP_ARCH_S390X,
    };
    const arch_set arch_sets[] = {
        {"LE", le_arch_tags, sizeof le_arch_tags / sizeof *le_arch_tags},
        {"BE", be_arch_tags, sizeof be_arch_tags / sizeof *be_arch_tags},
    };
    int rc = -1;

    for (size_t i = 0; i < sizeof arch_sets / sizeof *arch_sets; ++i) {
        const arch_set *as = &arch_sets[i];
        int sc_err;
        int fd = -1;
        int fname_len;
        char fname[PATH_MAX];

        scmp_filter_ctx ctx = NULL;
        ctx = seccomp_init(SCMP_ACT_ALLOW);
        if (ctx == NULL) {
            showerr("cannot construct seccomp context");
            return -rc;
        }
        sc_err = populate_filter(ctx, as->arch_tags, as->num_arch_tags);
        if (sc_err < 0) {
            seccomp_release(ctx);
            return -rc;
        }

        /* Save pseudo-code program */
        fname_len = snprintf(fname, sizeof fname, "%s-blacklist.pfc", as->name);
        if (fname_len < 0 || fname_len >= sizeof fname) {
            showerr("cannot format file name (%s)", as->name);
            seccomp_release(ctx);
            return -rc;
        }
        fd = open(fname, O_CREAT | O_TRUNC | O_WRONLY | O_NOFOLLOW, 0644);
        if (fd < 0) {
            showerr("cannot open file %s", fname);
            seccomp_release(ctx);
            return -rc;
        }
        sc_err = seccomp_export_pfc(ctx, fd);
        if (sc_err < 0) {
            showerr("cannot export PFC program %s", fname);
            seccomp_release(ctx);
            close(fd);
            return -rc;
        }

        close(fd);

        /* Save binary program. */
        fname_len = snprintf(fname, sizeof fname, "%s-blacklist.bpf", as->name);
        if (fname_len < 0 || fname_len >= sizeof fname) {
            showerr("cannot format file name (%s)", as->name);
            seccomp_release(ctx);
            return -rc;
        }
        fd = open(fname, O_CREAT | O_TRUNC | O_WRONLY | O_NOFOLLOW, 0644);
        if (fd < 0) {
            showerr("cannot open file %s", fname);
            seccomp_release(ctx);
            return -rc;
        }
        sc_err = seccomp_export_bpf(ctx, fd);
        if (sc_err < 0) {
            showerr("cannot export BPF program %s", fname);
            seccomp_release(ctx);
            close(fd);
            return -rc;
        }

        close(fd);
        seccomp_release(ctx);
    }
    rc = 0;
    return -rc;
}
