#include <unistd.h>
#include <stdio.h>
#include <stdlib.h>
#include <linux/sched.h>
#include <sys/mount.h>
#include <sys/apparmor.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <errno.h>
#include <sched.h>
#include <string.h>

#include "overlay.h"
#include "utils.h"
#include "seccomp.h"

int unshare(int flags);

void setup_devices_cgroup(const char *appname) {
   // create devices cgroup controller
   char cgroup_dir[128];
   if(snprintf(cgroup_dir, sizeof(cgroup_dir), "/sys/fs/cgroup/devices/snappy.%s/", appname) < 0)
      die("snprintf failed");

   struct stat statbuf;
   if (stat(cgroup_dir, &statbuf) != 0)
      if (mkdir(cgroup_dir, 0755) < 0)
         die("mkdir failed");

   // move ourselves into it
   char cgroup_file[128];
   if(snprintf(cgroup_file, sizeof(cgroup_file), "%s%s", cgroup_dir, "tasks") < 0)
      die("snprintf failed (2)");

   char buf[128];
   if (snprintf(buf, sizeof(buf), "%i", getpid()) < 0)
      die("snprintf failed (3)");
   write_string_to_file(cgroup_file, buf);

   // deny by default
   if(snprintf(cgroup_file, sizeof(cgroup_file), "%s%s", cgroup_dir, "devices.deny") < 0)
      die("snprintf failed (4)");
   write_string_to_file(cgroup_file, "a");
   
}

int main(int argc, char **argv)
{
   const int NR_ARGS = 4;
   if(argc < NR_ARGS+1)
       die("Usage: %s <rootdir> <appname> <binary> <apparmor>", argv[0]);

    if(geteuid() != 0)
       die("need to run as root or suid");

    const char *rootdir = argv[1];
    const char *appname = argv[2];
    const char *aa_profile = argv[3];
    const char *binary = argv[4];

    setup_devices_cgroup(appname);

    int i = 0;
    int rc = 0;
    
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

