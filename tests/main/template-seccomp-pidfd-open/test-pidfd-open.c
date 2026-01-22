#define _GNU_SOURCE
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/syscall.h>
#include <unistd.h>

int main(void) {
    pid_t pid;
    int fd;

    // Get our own PID
    pid = getpid();
    printf("Testing pidfd_open with PID %d\n", pid);

    // Try to open a pidfd for our own process
    fd = syscall(__NR_pidfd_open, pid, 0);

    if (fd == -1) {
        if (errno == ENOSYS) {
            printf("pidfd_open: not supported by kernel\n");
            return 0;
        } else if (errno == EPERM || errno == EACCES) {
            printf("pidfd_open: blocked (errno=%d: %s)\n", errno, strerror(errno));
            return 1;
        } else {
            printf("pidfd_open: failed with unexpected error (errno=%d: %s)\n", errno, strerror(errno));
            return 1;
        }
    }

    printf("pidfd_open: success (fd=%d)\n", fd);
    close(fd);
    return 0;
}
