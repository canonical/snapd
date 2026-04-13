#define _GNU_SOURCE
#include <stdio.h>
#include <sys/syscall.h>
#include <linux/personality.h>
#include <errno.h>

int main(void) {
    long res;

    /* Call personality() directly via syscall to bypass any libc wrapper */
    res = syscall(__NR_personality, ADDR_NO_RANDOMIZE);
    printf("personality(ADDR_NO_RANDOMIZE): %ld (%m)\n", res);
    if (res == 0) {
        /* Reset to avoid side-effects on subsequent calls */
        syscall(__NR_personality, 0);
    }

    res = syscall(__NR_personality, READ_IMPLIES_EXEC);
    printf("personality(READ_IMPLIES_EXEC): %ld (%m)\n", res);
    if (res == 0) {
        syscall(__NR_personality, 0);
    }

    return 0;
}
