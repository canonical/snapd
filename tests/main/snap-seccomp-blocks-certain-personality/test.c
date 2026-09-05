#define _GNU_SOURCE
#include <stdio.h>
#include <unistd.h>
#include <sys/syscall.h>
#include <linux/personality.h>
#include <errno.h>
#include <string.h>

static const char *errno_name(int err) {
    switch (err) {
    case EPERM:  return "EPERM";
    case EACCES: return "EACCES";
    case ESRCH:  return "ESRCH";
    default:     return strerror(err);
    }
}

int main(void) {
    long res;

    /* Call personality() directly via syscall to bypass any libc wrapper.
     * The seccomp deny rule (~) always returns EACCES. */
    errno = 0;
    res = syscall(__NR_personality, ADDR_NO_RANDOMIZE);
    if (res == -1) {
        printf("personality(ADDR_NO_RANDOMIZE): %ld (%s)\n", res, errno_name(errno));
    } else {
        printf("personality(ADDR_NO_RANDOMIZE): %ld (success)\n", res);
    }

    errno = 0;
    res = syscall(__NR_personality, READ_IMPLIES_EXEC);
    if (res == -1) {
        printf("personality(READ_IMPLIES_EXEC): %ld (%s)\n", res, errno_name(errno));
    } else {
        printf("personality(READ_IMPLIES_EXEC): %ld (success)\n", res);
    }

    return 0;
}
