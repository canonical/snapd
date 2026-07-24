#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <unistd.h>

static const char *errno_name(int e) __attribute__((unused));
static const char *errno_name(int e)
{
    switch (e) {
    case EACCES:     return "EACCES";
    case EPERM:      return "EPERM";
    case ENOSYS:     return "ENOSYS";
    case EOPNOTSUPP: return "EOPNOTSUPP";
    case EINVAL:     return "EINVAL";
    case ENOENT:     return "ENOENT";
    case EEXIST:     return "EEXIST";
    default:         return strerror(e);
    }
}

/* Test SYS_open with O_CREAT.  On architectures without SYS_open (e.g. arm64)
 * the test is skipped; SYS_openat (always present) provides the coverage
 * for those architectures via test_openat() below. */
static void test_open(const char *dir, const char *name, int mode, const char *label)
{
    char path[4096];
    snprintf(path, sizeof(path), "%s/%s", dir, name);
    unlink(path);

#ifdef SYS_open
    int fd = (int)syscall(SYS_open, path, O_CREAT | O_WRONLY, mode);
    if (fd < 0) {
        printf("open %s: %s\n", label, errno_name(errno));
    } else {
        printf("open %s: succeeded\n", label);
        close(fd);
        unlink(path);
    }
#else
    (void)mode;
    printf("open %s: skipped (no SYS_open)\n", label);
#endif
}

/* Test SYS_openat with O_CREAT.  SYS_openat is present on all supported
 * architectures and is always exercised. */
static void test_openat(const char *dir, const char *name, int mode, const char *label)
{
    char path[4096];
    snprintf(path, sizeof(path), "%s/%s", dir, name);
    unlink(path);

    int fd = (int)syscall(SYS_openat, AT_FDCWD, path, O_CREAT | O_WRONLY, mode);
    if (fd < 0) {
        printf("openat %s: %s\n", label, errno_name(errno));
    } else {
        printf("openat %s: succeeded\n", label);
        close(fd);
        unlink(path);
    }
}

/* Materialise an O_TMPFILE fd into the filesystem via /proc/self/fd, stat it
 * to confirm the actual mode bits, then clean up.
 * 'name' is used both as the syscall label in output and in the dest filename. */
static void finish_tmpfile(int fd, const char *dir, const char *name, const char *label)
{
    char procpath[64];
    char destpath[4096];
    snprintf(procpath, sizeof(procpath), "/proc/self/fd/%d", fd);
    snprintf(destpath, sizeof(destpath), "%s/tmpfile-%s-%s", dir, name, label);

    if (linkat(AT_FDCWD, procpath, AT_FDCWD, destpath, AT_SYMLINK_FOLLOW) < 0) {
        printf("%s tmpfile %s: linkat: %s\n", name, label, errno_name(errno));
    } else {
        struct stat st;
        if (stat(destpath, &st) < 0) {
            printf("%s tmpfile %s: stat: %s\n", name, label, errno_name(errno));
            unlink(destpath);
            close(fd);
            return;
        }
        printf("%s tmpfile %s: succeeded (mode=%04o)\n", name, label,
               (unsigned)(st.st_mode & 07777));
        unlink(destpath);
    }
    close(fd);
}

/* Test SYS_open with O_TMPFILE.  Skipped on architectures without SYS_open;
 * EOPNOTSUPP/EINVAL mean the filesystem does not support O_TMPFILE (not a
 * security failure — seccomp denials always produce EACCES). */
static void test_open_tmpfile(const char *dir, int mode, const char *label)
{
#ifdef SYS_open
    int fd = (int)syscall(SYS_open, dir, O_TMPFILE | O_WRONLY, mode);
    if (fd < 0) {
        if (errno == EOPNOTSUPP || errno == EINVAL) {
            printf("open tmpfile %s: skipped (O_TMPFILE not supported)\n", label);
        } else {
            printf("open tmpfile %s: %s\n", label, errno_name(errno));
        }
        return;
    }
    finish_tmpfile(fd, dir, "open", label);
#else
    (void)dir;
    (void)mode;
    printf("open tmpfile %s: skipped (no SYS_open)\n", label);
#endif
}

/* SYS_openat2 (Linux 5.6+) passes flags and mode inside a struct open_how
 * pointer.  Seccomp cannot inspect data behind a pointer, so the syscall must
 * be blocked entirely — even a request for a normal mode (0644) is denied.
 * On older kernels SYS_openat2 is absent from the headers (#ifdef guard) or
 * returns ENOSYS at runtime; both cases are reported as "skipped". */
struct open_how_t {
    uint64_t flags;
    uint64_t mode;
    uint64_t resolve;
};

static void test_openat2(const char *dir, const char *name, int mode, const char *label)
{
    char path[4096];
    snprintf(path, sizeof(path), "%s/%s", dir, name);
    unlink(path);

#ifdef SYS_openat2
    struct open_how_t how = {
        .flags   = O_CREAT | O_WRONLY,
        .mode    = (uint64_t)mode,
        .resolve = 0,
    };
    int fd = (int)syscall(SYS_openat2, AT_FDCWD, path, &how, sizeof(how));
    if (fd < 0) {
        if (errno == ENOSYS) {
            printf("openat2 %s: skipped (no SYS_openat2)\n", label);
        } else {
            printf("openat2 %s: %s\n", label, errno_name(errno));
        }
        return;
    }
    printf("openat2 %s: succeeded\n", label);
    close(fd);
    unlink(path);
#else
    (void)mode;
    printf("openat2 %s: skipped (no SYS_openat2)\n", label);
#endif
}

/* Test SYS_openat with O_TMPFILE.  Always exercised. */
static void test_openat_tmpfile(const char *dir, int mode, const char *label)
{
    int fd = (int)syscall(SYS_openat, AT_FDCWD, dir, O_TMPFILE | O_WRONLY, mode);
    if (fd < 0) {
        if (errno == EOPNOTSUPP || errno == EINVAL) {
            printf("openat tmpfile %s: skipped (O_TMPFILE not supported)\n", label);
        } else {
            printf("openat tmpfile %s: %s\n", label, errno_name(errno));
        }
        return;
    }
    finish_tmpfile(fd, dir, "openat", label);
}

int main(int argc, char *argv[])
{
    if (argc < 2) {
        fprintf(stderr, "Usage: %s <dir>\n", argv[0]);
        return 1;
    }

    /* SYS_open with O_CREAT (skipped on architectures without SYS_open) */
    test_open(argv[1], "test-open-suid",   04644, "setuid");
    test_open(argv[1], "test-open-sgid",   02644, "setgid");
    test_open(argv[1], "test-open-normal", 00644, "normal");

    /* SYS_openat with O_CREAT (always exercised) */
    test_openat(argv[1], "test-openat-suid",   04644, "setuid");
    test_openat(argv[1], "test-openat-sgid",   02644, "setgid");
    test_openat(argv[1], "test-openat-normal", 00644, "normal");

    /* SYS_open with O_TMPFILE + linkat (skipped without SYS_open) */
    test_open_tmpfile(argv[1], 04644, "setuid");
    test_open_tmpfile(argv[1], 02644, "setgid");
    test_open_tmpfile(argv[1], 00644, "normal");

    /* SYS_openat with O_TMPFILE + linkat (always exercised) */
    test_openat_tmpfile(argv[1], 04644, "setuid");
    test_openat_tmpfile(argv[1], 02644, "setgid");
    test_openat_tmpfile(argv[1], 00644, "normal");

    /* SYS_openat2: entirely blocked because seccomp cannot inspect open_how;
     * skipped on kernels older than 5.6 where the syscall is absent.
     * Unlike openat, even a normal-mode request is denied. */
    test_openat2(argv[1], "test-openat2-suid",   04644, "setuid");
    test_openat2(argv[1], "test-openat2-sgid",   02644, "setgid");
    test_openat2(argv[1], "test-openat2-normal",  00644, "normal");

    return 0;
}
