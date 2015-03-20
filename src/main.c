#include <unistd.h>
#include <stdio.h>
#include <stdlib.h>
#include <linux/sched.h>
#include <sys/mount.h>
#include <sys/apparmor.h>

#include "overlay.h"
#include "utils.h"
#include "seccomp.h"

int main(int argc, char **argv)
{
    int i = 0;

    const int NR_ARGS = 3;
    if(argc < NR_ARGS+1)
    {
       fprintf(stderr, "Usage: %s <rootdir> <binary> <apparmor>\n", argv[0]);
       exit(1);
    }

    const char *rootdir = argv[1];
    const char *binary = argv[2];
    const char *aa_profile = argv[3];

    //https://wiki.ubuntu.com/SecurityTeam/Specifications/SnappyConfinement#ubuntu-snapp-launch

    // setup env
    setenv("SNAP_APP_DIR", rootdir, 1);

#if 0 // not working
    // private tmp
    int rc = unshare(CLONE_NEWNS);
    if (rc != 0) {
       fprintf(stderr, "unshare failed %i", rc);
       exit(1);
    }
    if (!make_private_tmp())
       die("failed to create private /tmp dir");
#endif
    
    // FIXME: setup cgroup for net_cls

    // FIXME: setup iptables security table

    // FIXME: port binding restriction (seccomp?)

    // FIXME: ensure user specific data dir is availble (create if needed)

    // set seccomp
    seccomp_load_filters(aa_profile);
    
    // set apparmor rules
    aa_change_onexec(aa_profile);

    char **new_argv = malloc((argc-NR_ARGS+1)*sizeof(char*));
    new_argv[0] = (char*)binary;
    for(i=1; i < argc-NR_ARGS; i++)
       new_argv[i] = argv[i+NR_ARGS];
    new_argv[i] = NULL;
    
    return execv(binary, new_argv);
}
