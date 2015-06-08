/*
 * Copyright (C) 2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */
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
#include <fcntl.h>

#include "libudev.h"

#include "utils.h"
#include "seccomp.h"

#define MAX_BUF 1000

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
         must_snprintf(buf, sizeof(buf), "%u:%u", major, minor);
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

   // extra paranoia
   if(!verify_appname(appname))
      die("appname %s not allowed", appname);

   // create devices cgroup controller
   char cgroup_dir[PATH_MAX];
   must_snprintf(cgroup_dir, sizeof(cgroup_dir), "/sys/fs/cgroup/devices/snappy.%s/", appname);

   if (mkdir(cgroup_dir, 0755) < 0 && errno != EEXIST)
         die("mkdir failed");

   // move ourselves into it
   char cgroup_file[PATH_MAX];
   must_snprintf(cgroup_file, sizeof(cgroup_file), "%s%s", cgroup_dir, "tasks");

   char buf[128];
   must_snprintf(buf, sizeof(buf), "%i", getpid());
   write_string_to_file(cgroup_file, buf);

   // deny by default
   must_snprintf(cgroup_file, sizeof(cgroup_file), "%s%s", cgroup_dir, "devices.deny");
   write_string_to_file(cgroup_file, "a");

}

bool snappy_udev_setup_required(const char *appname) {
   debug("snappy_udev_setup_required");

   // extra paranoia
   if(!verify_appname(appname))
      die("appname %s not allowed", appname);

   char override_file[PATH_MAX];
   must_snprintf(override_file, sizeof(override_file), "/var/lib/apparmor/clicks/%s.json.additional", appname);

   // if a snap package gets unrestricted apparmor access we need to setup
   // a device cgroup.
   //
   // the "needle" string is what gives this access so we search for that
   // here
   const char *needle =
      "{"                          "\n"
      " \"write_path\": ["         "\n"
      "   \"/dev/**\""             "\n"
      " ],"                        "\n"
      " \"read_path\": ["          "\n"
      "   \"/run/udev/data/*\""     "\n"
      " ]\n"
      "}";
   debug("looking for: '%s'", needle);
   char content[strlen(needle)];

   int fd = open(override_file, O_CLOEXEC | O_NOFOLLOW | O_RDONLY);
   if (fd < 0)
      return false;
   int n = read(fd, content, sizeof(content));
   close(fd);
   if (n < sizeof(content))
      return false;

   // memcpy so that we don't have to deal with \0 in the input
   if (memcmp(content, needle, strlen(needle)) == 0) {
      debug("found needle, need to apply udev setup");
      return true;
   }

   return false;
}

void setup_private_mount(const char* appname) {
    uid_t uid = getuid();
    gid_t gid = getgid();
    char tmpdir[MAX_BUF] = {0};

    // Create a 0700 base directory, this is the base dir that is
    // protected from other users.
    // 
    // Under that basedir, we put a 1777 /tmp dir that is then bind
    // mounted for the applications to use
    must_snprintf(tmpdir, sizeof(tmpdir), "/tmp/snap.%d_%s_XXXXXX", uid, appname);
    if (mkdtemp(tmpdir) == NULL) {
        die("unable to create tmpdir");
    }

    // now we create a 1777 /tmp inside our private dir
    mode_t old_mask = umask(0);
    char *d = strdup(tmpdir);
    must_snprintf(tmpdir, sizeof(tmpdir), "%s/tmp", d);
    free(d);
    if (mkdir(tmpdir, 01777) != 0) {
       die("unable to create /tmp inside private dir");
    }
    umask(old_mask);
    
    // unshare() and CLONE_NEWNS require linux >= 2.6.16 and glibc >= 2.14
    // if using an older glibc, you'd need -D_BSD_SOURCE or -D_SVID_SORUCE.
    if (unshare(CLONE_NEWNS) < 0) {
        die("unable to set up mount namespace");
    }

    // MS_PRIVATE needs linux > 2.6.11
    if (mount("none", "/tmp", NULL, MS_PRIVATE, NULL) != 0) {
        die("unable to make /tmp/ private");
    }

    // MS_BIND is there from linux 2.4
    if (mount(tmpdir, "/tmp", NULL, MS_BIND, NULL) != 0) {
        die("unable to bind private /tmp");
    }

    // do the chown after the bind mount to avoid potential shenanigans
    if (chown("/tmp/", uid, gid) < 0) {
        die("unable to chown tmpdir");
    }

    // ensure we set the various TMPDIRs to our newly created tmpdir
    const char *tmpd[] = {"TMPDIR", "TEMPDIR", "SNAP_APP_TMPDIR",
                          // deprecated
                          "SNAPP_APP_TMPDIR",
                          NULL};
    int i;
    for (i=0; tmpd[i] != NULL; i++) {
       if (setenv(tmpd[i], "/tmp", 1) != 0) {
          die("unable to set '%s'", tmpd[i]);
       }
    }
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
       // verify binary path
       char apps_prefix[128];
       char frameworks_prefix[128];
       char oem_prefix[128];
       must_snprintf(apps_prefix, sizeof(apps_prefix), "/apps/%s/", appname);
       must_snprintf(frameworks_prefix, sizeof(frameworks_prefix), "/frameworks/%s/", appname);
       must_snprintf(oem_prefix, sizeof(oem_prefix), "/oem/%s/", appname);
       if (strstr(binary, apps_prefix) != binary &&
               strstr(binary, oem_prefix) != binary &&
               strstr(binary, frameworks_prefix) != binary)
          die("binary must be inside /apps/%s/, /frameworks/%s/ or /oem/%s/",
                  appname, appname, appname);

       // set up private mounts
       setup_private_mount(appname);

       // this needs to happen as root
       if(snappy_udev_setup_required(appname)) {
          setup_devices_cgroup(appname);
          setup_udev_snappy_assign(appname);
       }

       // the rest does not so drop privs back to calling user
       unsigned real_uid = getuid();
       unsigned real_gid = getgid();

       // Note that we do not call setgroups() here because its ok
       // that the user keeps the groups he already belongs to
       if (setgid(real_gid) != 0)
          die("setgid failed");
       if (setuid(real_uid) != 0)
          die("setuid failed");

       if(real_gid != 0 && (getuid() == 0 || geteuid() == 0))
          die("dropping privs did not work");
       if(real_uid != 0 && (getgid() == 0 || getegid() == 0))
          die("dropping privs did not work");
    }

    //https://wiki.ubuntu.com/SecurityTeam/Specifications/SnappyConfinement#ubuntu-snapp-launch

    int rc = 0;
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

    // and exec the new binary
    argv[NR_ARGS] = (char*)binary,
    execv(binary, (char *const*)&argv[NR_ARGS]);
    perror("execv failed");
    return 1;
}

