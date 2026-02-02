#define _GNU_SOURCE
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/syscall.h>
#include <sys/wait.h>
#include <linux/wait.h>
#include <unistd.h>

int main(void) {
    pid_t pid;
    int fd;
    siginfo_t info;

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

    // Now test opening pidfd for PID 1 and waitid
    printf("\nTesting pidfd_open with PID 1\n");
    fd = syscall(__NR_pidfd_open, 1, 0);

    if (fd == -1) {
        if (errno == ENOSYS) {
            printf("pidfd_open: not supported by kernel\n");
            return 0;
        } else if (errno == EPERM || errno == EACCES) {
            printf("pidfd_open for PID 1: blocked (errno=%d: %s)\n", errno, strerror(errno));
            return 1;
        } else {
            printf("pidfd_open for PID 1: failed with unexpected error (errno=%d: %s)\n", errno, strerror(errno));
            return 1;
        }
    }

    printf("pidfd_open for PID 1: success (fd=%d)\n", fd);

    // Try to waitid on PID 1 (should fail since it's not a child process)
    printf("Attempting waitid on PID 1 (should fail since it's not a child)...\n");
    int ret = waitid(P_PIDFD, fd, &info, WEXITED);

    if (ret == -1) {
        if (errno == ECHILD) {
            printf("waitid: correctly failed with ECHILD (PID 1 is not a child process)\n");
            close(fd);
            return 0;
        } else {
            printf("waitid: failed with unexpected error (errno=%d: %s)\n", errno, strerror(errno));
            close(fd);
            return 1;
        }
    } else {
        printf("waitid: unexpectedly succeeded\n");
        close(fd);
        return 1;
    }
}
