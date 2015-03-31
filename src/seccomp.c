#include <errno.h>
#include <stdio.h>
#include <unistd.h>
#include <string.h>
#include <ctype.h>
#include <stdlib.h>
#include <sys/prctl.h>

#include <seccomp.h>

#include "utils.h"

char *filter_profile_dir = "/var/lib/snappy/seccomp/profiles/";

// strip whitespace from the end of the given string (inplace)
void trim_right(char *s) {
   int end = strlen(s)-1;
   while(end >= 0 && isspace(s[end])) {
      s[end] = 0;
      end--;
   }
}

int seccomp_load_filters(const char *filter_profile)
{
   int rc = 0;
   int syscall_nr = -1;
   scmp_filter_ctx ctx = NULL;
   FILE *f = NULL;

   ctx = seccomp_init(SCMP_ACT_KILL);
   if (ctx == NULL)
      return ENOMEM;

   if (getenv("SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR") != NULL)
      filter_profile_dir = getenv("SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR");

   char profile_path[128];
   if (snprintf(profile_path, sizeof(profile_path), "%s/%s", filter_profile_dir, filter_profile) < 0) {
      goto out;
   }

   f = fopen(profile_path, "r");
   if (f == NULL) {
      fprintf(stderr, "Can not open %s (%s)\n", profile_path, strerror(errno));
      return -1;
   }
   char buf[80];
   while (fgets(buf, sizeof(buf), f) != NULL)
   {
      // comment, ignore
      if(buf[0] == '#')
         continue;

      // kill final newline
      trim_right(buf);
      if (strlen(buf) == 0)
         continue;

      // check for special "@unrestricted" command
      if (strncmp(buf, "@unrestricted", sizeof(buf)) == 0)
         goto out;

      syscall_nr = seccomp_syscall_resolve_name(buf);
      // syscall not available on this arch/kernel
      if (syscall_nr == __NR_SCMP_ERROR)
         continue;

      // a normal line with a syscall
      rc = seccomp_rule_add_exact(ctx, SCMP_ACT_ALLOW, syscall_nr, 0);
      if (rc != 0) {
         rc = seccomp_rule_add(ctx, SCMP_ACT_ALLOW, syscall_nr, 0);
	 if (rc != 0) {
             fprintf(stderr, "seccomp_rule_add failed with %i for '%s'\n", rc, buf);
             goto out;
	 }
      }
   }

   // Make sure we can't elevate later
   if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)) {
      perror("prctl(NO_NEW_PRIVS)");
      goto out;
   }

   // load it into the kernel
   rc = seccomp_load(ctx);
   if (rc != 0) {
      fprintf(stderr, "seccomp_load failed with %i\n", rc);
      goto out;
   }


 out:
   if (f != NULL) {
      fclose(f);
   }
   seccomp_release(ctx);
   return rc;
}
