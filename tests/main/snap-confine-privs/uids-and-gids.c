#define _GNU_SOURCE
#include <errno.h>
#include <grp.h>
#include <pwd.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/types.h>
#include <unistd.h>

int main(int argc __attribute__((unused)), char* argv[] __attribute__((unused)))
{
    uid_t ruid, euid, suid;
    gid_t rgid, egid, sgid;
    if (getresuid(&ruid, &euid, &suid) < 0) {
        perror("cannot call getresuid");
        exit(1);
    }
    if (getresgid(&rgid, &egid, &sgid) < 0) {
        perror("cannot call getresgid");
        exit(1);
    }
    printf("ruid=%-5d euid=%-5d suid=%-5d rgid=%-5d egid=%-5d sgid=%-5d\n", ruid, euid, suid, rgid, egid, sgid);
    return 0;
}
