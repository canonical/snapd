#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/syscall.h>
#include <termios.h>
#include <unistd.h>

static int ioctl64(int fd, unsigned long nr, unsigned long arg) {
    errno = 0;
    return syscall(__NR_ioctl, fd, nr, arg);
}

int main(int argc, char **argv) {
    int res;
    int saved_errno;
    char pushmeback = '#';

    int fd = open("/dev/null", O_RDWR);
    int rc = EXIT_FAILURE;
    if (fd < 0) {
        perror("cannot open /dev/null");
        return rc;
    }

    if (argc == 2 && strcmp(argv[1], "--evil") == 0) {
        res = ioctl64(fd, TIOCSTI, (unsigned long)&pushmeback);
        saved_errno = errno;
        // The seccomp profile contains an explicit denial so we get EACCESS instead of EPERM.
        printf("normal TIOCSTI: %d (%m) (expect EACCES)\n", res);
        if (res < 0 && saved_errno == EACCES) {
            rc = EXIT_SUCCESS;
        }
    } else if (argc == 2 && strcmp(argv[1], "--evil-high") == 0) {
        res = ioctl64(fd, TIOCSTI | (1UL << 32UL), (unsigned long)&pushmeback);
        saved_errno = errno;
        printf("high-bit-set TIOCSTI: %d (%m) (expect EACCES)\n", res);
        if (res < 0 && saved_errno == EACCES) {
            rc = EXIT_SUCCESS;
        }
    } else if (argc == 2 && strcmp(argv[1], "--good") == 0) {
        res = ioctl64(fd, TCFLSH, TCIOFLUSH);
        saved_errno = errno;
        printf("unrelated TCFLSH: %d (%m) (expect ENOTTY)\n", res);
        if (res < 0 && saved_errno == ENOTTY) {
            rc = EXIT_SUCCESS;
        }
    } else if (argc == 2 && strcmp(argv[1], "--good-high") == 0) {
        res = ioctl64(fd, TCFLSH | (1UL << 32UL), TCIOFLUSH);
        saved_errno = errno;
        printf("unrelated TCFLSH: %d (%m) (expect ENOTTY)\n", res);
        if (res < 0 && saved_errno == ENOTTY) {
            rc = EXIT_SUCCESS;
        }
    } else {
        printf("Usage: lp-1812973 --{evil,good}{,-high}\n");
        rc = EXIT_SUCCESS;
    }
    close(fd);
    return rc;
}
