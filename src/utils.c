#include<stdio.h>
#include<stdlib.h>
#include<stdarg.h>
#include<string.h>
#include<stdio.h>

#include "utils.h"

void die(const char *msg, ...)
{
   va_list va;
   va_start(va, msg);
   vfprintf(stderr, msg, va);
   va_end(va);

   fprintf(stderr, "\n");
   exit(1);
}

bool error(const char *msg, ...)
{
   va_list va;
   va_start(va, msg);
   vfprintf(stderr, msg, va);
   va_end(va);

   return false;
}

void debug(const char *msg, ...)
{
   if(getenv("UBUNTU_CORE_LAUNCHER_DEBUG") == NULL)
      return;

   va_list va;
   va_start(va, msg);
   fprintf(stderr, "DEBUG: ");
   vfprintf(stderr, msg, va);
   fprintf(stderr, "\n");
   va_end(va);
}

void write_string_to_file(const char *filepath, const char *buf) {
   debug("write_string_to_file %s %s", filepath, buf);
   FILE *f = fopen(filepath, "w");
   if (f == NULL)
      die("fopen %s failed\n", filepath);
   if (fwrite(buf, strlen(buf), 1, f) != 1)
      die("fwrite failed");
   fclose(f);
}
