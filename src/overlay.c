#include <alloca.h>
#include <linux/sched.h>
#include <stdio.h>
#include <string.h>
#include <sys/mount.h>
#include <fcntl.h>
#include <unistd.h>
#include <sched.h>
#include <stdlib.h>

#include "utils.h"

// not available in glibc
int pivot_root(const char *new_root, const char *put_old);


bool make_overlay(const char* dirs[])
{
    int i = 0;
    int len = 0;
    char* options;

    // FIXME: don't use /mnt, use a special dir just for us
    for (i = 0; dirs[i] != NULL; i++) {
        if (i == 0) {
            len = strlen(dirs[i]) + strlen("upperdir=,lowerdir=/") + 2;
            options = alloca(len);
            snprintf(options, len, "upperdir=%s,lowerdir=/", dirs[i]);
        }
        else {
            len = strlen(dirs[i]) + strlen("upperdir=,lowerdir=/mnt") + 2;
            options = alloca(len);
            snprintf(options, len, "upperdir=%s,lowerdir=/mnt", dirs[i]);
        }

        if (mount("overlayfs", "/mnt", "overlayfs", MS_MGC_VAL, options) != 0)
           return error("failed to mount overlayfs");
    }

    if(chdir("/mnt") != 0)
       return error("Failed to chdir to /mnt");
    if(pivot_root(".", ".") != 0)
       //return error("Failed  pivot_root()");
       error("Failed  pivot_root()");
    if(chroot(".") != 0)
       return error("Failed to chroot to .");
    if(chdir("/") != 0)
       return error("Failed to chdir to /");

    return true;
}

bool make_private_tmp()
{
    char private_tmp_template[] = {"/tmp/ubuntu-core-launch-tmp-XXXXXX"};
    char *private_tmp = mkdtemp(private_tmp_template);
    if (private_tmp == NULL)
       return error("failed to create /tmp dir");
    if(mount(private_tmp, "/tmp/", "none", MS_BIND, "") != 0)
       error("mounting private /tmp failed");

    return true;
}
