#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <unistd.h>

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

static void test_mknod(const char *dir, const char *name, mode_t mode, const char *label)
{
    char path[4096];
    snprintf(path, sizeof(path), "%s/%s", dir, name);
    unlink(path);

#ifdef SYS_mknod
    /* Use S_IFIFO (named pipe) as the file type: creating FIFOs does not
     * require CAP_MKNOD, so it works in a confined snap without extra
     * privileges.
     * Call the raw mknod syscall directly to bypass any libc wrapper. */
    int ret = (int)syscall(SYS_mknod, path, S_IFIFO | mode, 0);
    if (ret < 0) {
        printf("mknod %s: %s\n", label, errno_name(errno));
    } else {
        printf("mknod %s: succeeded\n", label);
        unlink(path);
    }
#else
    /* mknod is not available on this architecture (e.g. arm64); skip. */
    (void)mode;
    printf("mknod %s: skipped (no SYS_mknod)\n", label);
#endif
}

static void test_mknodat(const char *dir, const char *name, mode_t mode, const char *label)
{
    char path[4096];
    snprintf(path, sizeof(path), "%s/%s", dir, name);
    unlink(path);

    /* Same as test_mknod but exercises the mknodat syscall. */
    int ret = (int)syscall(SYS_mknodat, AT_FDCWD, path, S_IFIFO | mode, 0);
    if (ret < 0) {
        printf("mknodat %s: %s\n", label, errno_name(errno));
    } else {
        printf("mknodat %s: succeeded\n", label);
        unlink(path);
    }
}

int main(int argc, char *argv[])
{
    if (argc < 2) {
        fprintf(stderr, "Usage: %s <dir>\n", argv[0]);
        return 1;
    }

    /* 04644 = setuid bit + rw-r--r-- */
    test_mknod(argv[1], "test-mknod-suid", 04644, "setuid");

    /* 02644 = setgid bit + rw-r--r-- */
    test_mknod(argv[1], "test-mknod-sgid", 02644, "setgid");

    /* 00644 = normal rw-r--r-- */
    test_mknod(argv[1], "test-mknod-normal", 00644, "normal");

    /* Same tests via mknodat */
    test_mknodat(argv[1], "test-mknodat-suid", 04644, "setuid");
    test_mknodat(argv[1], "test-mknodat-sgid", 02644, "setgid");
    test_mknodat(argv[1], "test-mknodat-normal", 00644, "normal");

    return 0;
}
