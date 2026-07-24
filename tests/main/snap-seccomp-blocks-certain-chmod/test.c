#include <errno.h>
#include <fcntl.h>
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

/* Create a plain file at path and return an open fd, or -1 on error. */
static int make_file(const char *path)
{
    unlink(path);
    int fd = open(path, O_CREAT | O_WRONLY, 0644);
    if (fd < 0) {
        fprintf(stderr, "open %s: %s\n", path, errno_name(errno));
    }
    return fd;
}

static void test_chmod(const char *path, mode_t mode, const char *label)
{
#ifdef SYS_chmod
    int ret = (int)syscall(SYS_chmod, path, mode);
    if (ret < 0) {
        printf("chmod %s: %s\n", label, errno_name(errno));
    } else {
        printf("chmod %s: succeeded\n", label);
    }
#else
    /* chmod is not available on this architecture; skip silently. */
    (void)path; (void)mode;
    printf("chmod %s: skipped (no SYS_chmod)\n", label);
#endif
}

static void test_fchmod(int fd, mode_t mode, const char *label)
{
    int ret = (int)syscall(SYS_fchmod, fd, mode);
    if (ret < 0) {
        printf("fchmod %s: %s\n", label, errno_name(errno));
    } else {
        printf("fchmod %s: succeeded\n", label);
    }
}

static void test_fchmodat(const char *path, mode_t mode, const char *label)
{
    int ret = (int)syscall(SYS_fchmodat, AT_FDCWD, path, mode, 0);
    if (ret < 0) {
        printf("fchmodat %s: %s\n", label, errno_name(errno));
    } else {
        printf("fchmodat %s: succeeded\n", label);
    }
}

static void test_fchmodat2(const char *path, mode_t mode, const char *label)
{
#ifdef SYS_fchmodat2
    int ret = (int)syscall(SYS_fchmodat2, AT_FDCWD, path, mode, 0);
    if (ret < 0) {
        printf("fchmodat2 %s: %s\n", label, errno_name(errno));
    } else {
        printf("fchmodat2 %s: succeeded\n", label);
    }
#else
    /* fchmodat2 may not be available on older kernels; skip silently. */
    (void)path; (void)mode;
    printf("fchmodat2 %s: skipped (no SYS_fchmodat2)\n", label);
#endif
}

int main(int argc, char *argv[])
{
    if (argc < 2) {
        fprintf(stderr, "Usage: %s <dir>\n", argv[0]);
        return 1;
    }

    char path[4096];
    int fd;

    /* --- chmod --- */
    snprintf(path, sizeof(path), "%s/test-chmod", argv[1]);
    fd = make_file(path);
    if (fd < 0) return 1;
    close(fd);

    test_chmod(path, 04644, "setuid");  /* 04644: setuid + rw-r--r-- */
    test_chmod(path, 02644, "setgid");  /* 02644: setgid + rw-r--r-- */
    test_chmod(path, 01644, "sticky");  /* 01644: sticky + rw-r--r-- -- allowed */
    test_chmod(path, 00644, "normal");  /* 00644: plain rw-r--r-- -- allowed */
    unlink(path);

    /* --- fchmod --- */
    snprintf(path, sizeof(path), "%s/test-fchmod", argv[1]);
    fd = make_file(path);
    if (fd < 0) return 1;

    test_fchmod(fd, 04644, "setuid");
    test_fchmod(fd, 02644, "setgid");
    test_fchmod(fd, 01644, "sticky");
    test_fchmod(fd, 00644, "normal");
    close(fd);
    unlink(path);

    /* --- fchmodat --- */
    snprintf(path, sizeof(path), "%s/test-fchmodat", argv[1]);
    fd = make_file(path);
    if (fd < 0) return 1;
    close(fd);

    test_fchmodat(path, 04644, "setuid");
    test_fchmodat(path, 02644, "setgid");
    test_fchmodat(path, 01644, "sticky");
    test_fchmodat(path, 00644, "normal");
    unlink(path);

    /* --- fchmodat2 --- */
    snprintf(path, sizeof(path), "%s/test-fchmodat2", argv[1]);
    fd = make_file(path);
    if (fd < 0) return 1;
    close(fd);

    test_fchmodat2(path, 04644, "setuid");
    test_fchmodat2(path, 02644, "setgid");
    test_fchmodat2(path, 01644, "sticky");
    test_fchmodat2(path, 00644, "normal");
    unlink(path);

    return 0;
}
