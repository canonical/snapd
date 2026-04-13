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
    res = syscall(__NR_personality, ADDR_NO_RANDOMIZE);
    printf("personality(ADDR_NO_RANDOMIZE): %ld (%s)\n", res, errno_name(errno));

    res = syscall(__NR_personality, READ_IMPLIES_EXEC);
    printf("personality(READ_IMPLIES_EXEC): %ld (%s)\n", res, errno_name(errno));

    return 0;
}
