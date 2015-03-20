#include <errno.h>
#include <stdio.h>
#include <unistd.h>
#include <string.h>
#include <ctype.h>

#include <seccomp.h>

#include "utils.h"

const char *filter_profile_dir = "/var/lib/security/seccomp/profiles/";

// strip whitespace from the end of the given string (inplace)
void trimRight(char *s) {
   int end = strlen(s)-1;
   while(end >= 0 && isspace(s[end])) {
      s[end] = 0;
      end--;
   }
}

int seccomp_load_filters(const char *filter_profile)
{
   int rc = 0;
   scmp_filter_ctx ctx = NULL;
   FILE *f = NULL;

   ctx = seccomp_init(SCMP_ACT_KILL);
   if (ctx == NULL)
      return ENOMEM;

   char profile_path[128];
   rc = snprintf(profile_path, sizeof(profile_path), "%s/%s", filter_profile_dir, filter_profile);
   if (rc < 0) {
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
      // kill final newline
      trimRight(buf);
      if (strlen(buf) == 0) {
         continue;
      }
      rc = seccomp_rule_add_exact(ctx, SCMP_ACT_ALLOW, 
                                  seccomp_syscall_resolve_name(buf), 0);
      if (rc != 0) {
         fprintf(stderr, "seccomp_rule_add_exact failed with %i for %s\n", rc, buf);
         goto out;
      }
   }

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
