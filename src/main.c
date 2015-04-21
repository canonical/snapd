#include <unistd.h>
#include <stdio.h>
#include <stdlib.h>
#include <limits.h>
#include <linux/sched.h>
#include <sys/mount.h>
#include <sys/apparmor.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <errno.h>
#include <sched.h>
#include <string.h>
#include <linux/kdev_t.h>
#include <stdlib.h>
#include <regex.h>
#include <grp.h>

#include "libudev.h"

#include "utils.h"
#include "seccomp.h"

int unshare(int flags);

void run_snappy_app_dev_add(struct udev *u, const char *path, const char *appname) {
   debug("run_snappy_app_dev_add: %s %s", path, appname);
      struct udev_device *d = udev_device_new_from_syspath(u, path);
      if (d == NULL)
         die("can not find %s", path);
      dev_t devnum = udev_device_get_devnum (d);
      udev_device_unref(d);

      int status = 0;
      pid_t pid = fork();
      if (pid == 0) {
         char buf[64];
         unsigned major = MAJOR(devnum);
         unsigned minor = MINOR(devnum);
         if(snprintf(buf, sizeof(buf), "%u:%u", major, minor) < 0)
            die("snprintf failed (5)");
         if(execl("/lib/udev/snappy-app-dev", "/lib/udev/snappy-app-dev", "add", appname, path, buf, NULL) != 0)
            die("execlp failed");
      }
      if(waitpid(pid, &status, 0) < 0)
         die("waitpid failed");
      if(WIFEXITED(status) && WEXITSTATUS(status) != 0)
         die("child exited with status %i", WEXITSTATUS(status));
      else if(WIFSIGNALED(status))
         die("child died with signal %i", WTERMSIG(status));
}

void setup_udev_snappy_assign(const char *appname) {
   debug("setup_udev_snappy_assign");

   struct udev *u = udev_new();
   if (u == NULL)
      die("udev_new failed");

   const char* static_devices[] = {
      "/sys/class/mem/null",
      "/sys/class/mem/full",
      "/sys/class/mem/zero",
      "/sys/class/mem/random",
      "/sys/class/mem/urandom",
      "/sys/class/tty/tty",
      "/sys/class/tty/console",
      "/sys/class/tty/ptmx",
      NULL,
   };
   int i;
   for(i=0; static_devices[i] != NULL; i++) {
      run_snappy_app_dev_add(u, static_devices[i], appname);
   }

   struct udev_enumerate *devices = udev_enumerate_new(u);
   if (devices == NULL)
      die("udev_enumerate_new failed");

   if (udev_enumerate_add_match_tag (devices, "snappy-assign") != 0)
      die("udev_enumerate_add_match_tag");

   if(udev_enumerate_add_match_property (devices, "SNAPPY_APP", appname) != 0)
      die("udev_enumerate_add_match_property");

   if(udev_enumerate_scan_devices(devices) != 0)
      die("udev_enumerate_scan failed");

   struct udev_list_entry *l = udev_enumerate_get_list_entry (devices);
   while (l != NULL) {
      const char *path = udev_list_entry_get_name (l);
      if (path == NULL)
         die("udev_list_entry_get_name failed");
      run_snappy_app_dev_add(u, path, appname);
      l = udev_list_entry_get_next(l);
   }

   udev_enumerate_unref(devices);
   udev_unref(u);
}

void setup_devices_cgroup(const char *appname) {
   debug("setup_devices_cgroup");

   // create devices cgroup controller
   char cgroup_dir[PATH_MAX];
   if(snprintf(cgroup_dir, sizeof(cgroup_dir), "/sys/fs/cgroup/devices/snappy.%s/", appname) < 0)
      die("snprintf failed");

   struct stat statbuf;
   if (stat(cgroup_dir, &statbuf) != 0)
      if (mkdir(cgroup_dir, 0755) < 0)
         die("mkdir failed");

   // move ourselves into it
   char cgroup_file[PATH_MAX];
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

bool verify_appname(const char *appname) {
   // these chars are allowed in a appname
   const char* whitelist_re = "^[a-z0-9][a-z0-9+._-]+$";
   regex_t re;
   if (regcomp(&re, whitelist_re, REG_EXTENDED|REG_NOSUB) != 0)
      die("can not compile regex %s", whitelist_re);

   int status = regexec(&re, appname, 0, NULL, 0);
   regfree(&re);

   return (status == 0);
}

int main(int argc, char **argv)
{
   const int NR_ARGS = 3;
   if(argc < NR_ARGS+1)
       die("Usage: %s <appname> <apparmor> <binary>", argv[0]);

   const char *appname = argv[1];
   const char *aa_profile = argv[2];
   const char *binary = argv[3];

   if(!verify_appname(appname))
      die("appname %s not allowed", appname);
   
   // this code always needs to run as root for the cgroup/udev setup,
   // however for the tests we allow it to run as non-root
   if(geteuid() != 0 && getenv("UBUNTU_CORE_LAUNCHER_NO_ROOT") == NULL) {
       die("need to run as root or suid");
   }

   if(geteuid() == 0) {
       // this needs to happen as root
       setup_devices_cgroup(appname);
       setup_udev_snappy_assign(appname);

       // the rest does not so drop privs back to user
       unsigned real_uid = getuid();
       unsigned real_gid = getgid();

       if (setgroups(1, &real_gid) != 0)
          die("setgid failed");
       if (setgid(real_gid) != 0)
          die("setgid failed");
       if (setuid(real_uid) != 0)
          die("seteuid failed");

       if(real_gid != 0 && (getuid() == 0 || geteuid() == 0))
          die("dropping privs did not work");
       if(real_uid != 0 && (getgid() == 0 || getegid() == 0))
          die("dropping privs did not work");
    }

    int i = 0;
    int rc = 0;

   //https://wiki.ubuntu.com/SecurityTeam/Specifications/SnappyConfinement#ubuntu-snapp-launch

    // set apparmor rules
    rc = aa_change_onexec(aa_profile);
    if (rc != 0) {
       if (getenv("SNAPPY_LAUNCHER_INSIDE_TESTS") == NULL)
          die("aa_change_onexec failed with %i\n", rc);
    }

    // set seccomp
    rc = seccomp_load_filters(aa_profile);
    if (rc != 0)
       die("seccomp_load_filters failed with %i\n", rc);

    // realloc args and exec the binary
    char **new_argv = malloc((argc-NR_ARGS+1)*sizeof(char*));
    new_argv[0] = (char*)binary;
    for(i=1; i < argc-NR_ARGS; i++)
       new_argv[i] = argv[i+NR_ARGS];
    new_argv[i] = NULL;

    return execv(binary, new_argv);
}

