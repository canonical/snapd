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

void write_string_to_file(const char *filepath, const char *buf) {
   FILE *f = fopen(filepath, "w");
   if (f == NULL)
      die("fopen %s failed\n", filepath);
   if (fwrite(buf, strlen(buf), 1, f) < 0)
      die("fwrite failed");
   fclose(f);
}
