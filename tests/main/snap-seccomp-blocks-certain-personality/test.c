#define _GNU_SOURCE
#include <stdio.h>
#include <unistd.h>
#include <sys/syscall.h>
#include <linux/personality.h>
#include <errno.h>

int main(void) {
    long res;

    /* Call personality() directly via syscall to bypass any libc wrapper.
     * The seccomp deny rule (~) always returns EACCES (13). */
    res = syscall(__NR_personality, ADDR_NO_RANDOMIZE);
    printf("personality(ADDR_NO_RANDOMIZE): %ld (%s)\n", res, errno == EACCES ? "EACCES" : "unexpected");

    res = syscall(__NR_personality, READ_IMPLIES_EXEC);
    printf("personality(READ_IMPLIES_EXEC): %ld (%s)\n", res, errno == EACCES ? "EACCES" : "unexpected");

    return 0;
}
