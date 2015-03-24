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
    int rc = 0;
    
    const int NR_ARGS = 3;
    if(argc < NR_ARGS+1)
    {
       fprintf(stderr, "Usage: %s <rootdir> <binary> <apparmor>\n", argv[0]);
       exit(1);
    }

    const char *rootdir = argv[1];
    const char *aa_profile = argv[2];
    const char *binary = argv[3];

    //https://wiki.ubuntu.com/SecurityTeam/Specifications/SnappyConfinement#ubuntu-snapp-launch

    // setup env
    setenv("SNAP_APP_DIR", rootdir, 1);

    if (getenv("SNAPPY_LAUNCHER_SKIP_APPARMOR") != NULL) {
       // set apparmor rules
       rc = aa_change_onexec(aa_profile);
       if (rc != 0) {
          fprintf(stderr, "aa_change_onexec failed with %i\n", rc);
          return 1;
       }
    }

    // set seccomp
    rc = seccomp_load_filters(aa_profile);
    if (rc != 0) {
       fprintf(stderr, "seccomp_load_filters failed with %i\n", rc);
       return 1;
    }

    // realloc args and exec the binary
    char **new_argv = malloc((argc-NR_ARGS+1)*sizeof(char*));
    new_argv[0] = (char*)binary;
    for(i=1; i < argc-NR_ARGS; i++)
       new_argv[i] = argv[i+NR_ARGS];
    new_argv[i] = NULL;
    
    return execv(binary, new_argv);
}
