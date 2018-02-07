#include<signal.h>
#include<stdio.h>
#include <stdlib.h>
#include<unistd.h>

int main(int argc, char **argv) {
   if (getenv("SNAP_CONFINE_DEBUG") != NULL) {
      for (int i=0;i<argc;i++) {
         printf("-%s-\n", argv[i]);
      }
   }

   // signal gdb to stop here
   printf("\n\nWelcome to `snap run --gdb`.\n");
   printf("You are right before your application is execed():\n");
   printf("- set any options you may need\n");
   printf("- use 'cont' to start\n");
   raise(SIGTRAP);

   const char *executable = argv[1];
   execv(executable, (char *const *)&argv[1]);
   perror("execv failed");
   return 10;

}
