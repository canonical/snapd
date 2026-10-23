#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/syscall.h>
#include <unistd.h>

/* Define struct open_how locally, <linux/openat2.h> may be missing. */
struct test_open_how {
    uint64_t flags;
    uint64_t mode;
    uint64_t resolve;
};

int main(void)
{
    const char *path = "/dev/null";

#ifdef SYS_openat2
    struct test_open_how how;
    memset(&how, 0, sizeof how);
    how.flags = O_RDONLY;
    int fd = (int)syscall(SYS_openat2, AT_FDCWD, path, &how, sizeof how);
    if (fd < 0) {
        if (errno == ENOSYS) {
            printf("openat2: ENOSYS\n");
        } else {
            printf("openat2: errno %d (%s)\n", errno, strerror(errno));
        }
    } else {
        printf("openat2: succeeded\n");
        close(fd);
    }
#else
    printf("openat2: skipped (no SYS_openat2)\n");
#endif

    /* Plain openat must keep working. */
    int fd2 = (int)syscall(SYS_openat, AT_FDCWD, path, O_RDONLY);
    if (fd2 < 0) {
        printf("openat: errno %d (%s)\n", errno, strerror(errno));
    } else {
        printf("openat: succeeded\n");
        close(fd2);
    }
    return 0;
}
