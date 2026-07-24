#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <sys/syscall.h>
#include <unistd.h>

static void test_creat(const char *dir, const char *name, int mode, const char *label)
{
    char path[4096];
    snprintf(path, sizeof(path), "%s/%s", dir, name);
    unlink(path);

#ifdef SYS_creat
    int fd = (int)syscall(SYS_creat, path, mode);
    if (fd < 0) {
        printf("creat %s: %s\n", label, strerror(errno));
    } else {
        printf("creat %s: succeeded\n", label);
        close(fd);
        unlink(path);
    }
#else
    /* creat is not available on this architecture (e.g. arm64); skip. */
    (void)mode;
    printf("creat %s: skipped (no SYS_creat)\n", label);
#endif
}

int main(int argc, char *argv[])
{
    if (argc < 2) {
        fprintf(stderr, "Usage: %s <dir>\n", argv[0]);
        return 1;
    }

    /* 04644 = setuid bit + rw-r--r-- */
    test_creat(argv[1], "test-creat-suid", 04644, "setuid");

    /* 02644 = setgid bit + rw-r--r-- */
    test_creat(argv[1], "test-creat-sgid", 02644, "setgid");

    /* 00644 = normal rw-r--r-- */
    test_creat(argv[1], "test-creat-normal", 00644, "normal");

    return 0;
}
