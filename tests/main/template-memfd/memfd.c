#define _GNU_SOURCE
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/syscall.h> /* SYS_* constants */
#include <unistd.h>

static int memfd_secret(unsigned int flags) { return syscall(SYS_memfd_secret, flags); }

static void fd_close(int *fd) {
    if (fd != NULL && *fd >= 0) {
        close(*fd);
        *fd = -1;
    }
}

int main(int argc, char *argv[]) {
    if (argc != 2) {
        fprintf(stderr, "usage: %s [secret|create]\n", argv[0]);
        return 1;
    }

    int fd __attribute__((cleanup(fd_close))) = -1;
    if (strcmp(argv[1], "secret") == 0) {
        fd = memfd_secret(0);
    } else if (strcmp(argv[1], "create") == 0) {
        fd = memfd_create("test", 0);
    } else {
        fprintf(stderr, "incorrect mode: '%s'\n", argv[1]);
        return 1;
    }

    if (fd < 0) {
        perror("memfd");
        return 1;
    }

    if (ftruncate(fd, 1024) != 0) {
        perror("ftruncate failed");
        return 1;
    }

    const char canary[] = "hello";
    size_t canary_len = strlen(canary);

    void *addr = mmap(NULL, canary_len, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    if (addr == NULL) {
        perror("map");
        return 1;
    }

    fd_close(&fd);

    strncpy(addr, canary, canary_len);

    if (strcmp(addr, canary) != 0) {
        fprintf(stderr, "unexpected data\n");
        return 1;
    }
    return 0;
}
