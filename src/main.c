#include <unistd.h>
#include <stdio.h>
#include <stdlib.h>
#include <linux/sched.h>
#include <sys/mount.h>

#include "overlay.h"
#include "utils.h"

int main(int argc, char **argv)
{
    int i = 0;

    if(argc < 3)
    {
       fprintf(stderr, "Usage: %s <rootdir> <binary>\n", argv[0]);
       exit(1);
    }

    const char *rootdir = argv[1];
    const char *binary = argv[2];

    //https://wiki.ubuntu.com/SecurityTeam/Specifications/SnappyConfinement#ubuntu-snapp-launch

    // setup env
    setenv("SNAP_APP_DIR", rootdir, 1);

    // setup mount namespace
    if (geteuid())
        unshare(CLONE_NEWUSER);
    unshare(CLONE_NEWNS);

    // FIXME: we need to add all frameworks that need to be overlayed here
    const char* OVERLAY_DIRS[] = {
       "/",
       NULL,
    };
    if (!make_overlay(OVERLAY_DIRS))
       die("Failed to setup overlay");
    if (!make_private_tmp())
       die("failed to create private /tmp dir");

   // FIXME: setup cgroup

   // FIXME: setup iptables

   // FIXME: seccomp

   // FIXME: ensure user specific data dir is availble (create if needed)

   // run the app
   chdir(rootdir);

   char **new_argv = malloc((argc-1)*sizeof(char*));
   new_argv[0] = (char*)binary;
   for(i=1; i < argc-2; i++)
      new_argv[i] = argv[i+2];
   new_argv[i] = NULL;

   execv(binary, new_argv);
   //execl("/bin/bash", "/bin/bash", NULL);
}
