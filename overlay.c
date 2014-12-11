#include <alloca.h>
#include <linux/sched.h>
#include <stdio.h>
#include <string.h>
#include <sys/mount.h>
#include <fcntl.h>
#include <unistd.h>

int main(int argc, char* argv[]) {
    int i = 0;
    int len = 0;
    int ret = 0;
    char* options;

    if (geteuid())
        unshare(CLONE_NEWUSER);
    unshare(CLONE_NEWNS);

    for (i = 1; i < argc; i++) {
        if (i == 1) {
            len = strlen(argv[i]) + strlen("upperdir=,lowerdir=/") + 2;
            options = alloca(len);
            ret = snprintf(options, len, "upperdir=%s,lowerdir=/", argv[i]);
        }
        else {
            len = strlen(argv[i]) + strlen("upperdir=,lowerdir=/mnt") + 2;
            options = alloca(len);
            ret = snprintf(options, len, "upperdir=%s,lowerdir=/mnt", argv[i]);
        }

        mount("overlayfs", "/mnt", "overlayfs", MS_MGC_VAL, options);
    }

    chdir("/mnt");
    pivot_root(".", ".");
    chroot(".");

    chdir("/");
    execl("/bin/bash", "/bin/bash", NULL);
}
