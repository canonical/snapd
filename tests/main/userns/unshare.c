// taken from man unshare(2)
/* unshare.c

   A simple implementation of the unshare(1) command: unshare
   namespaces and execute a command.
 */
#define _GNU_SOURCE
#include <err.h>
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

static void
usage(char *pname)
{
  fprintf(stderr, "Usage: %s [options] program [arg...]\n", pname);
  fprintf(stderr, "Options can be:\n");
  fprintf(stderr, "    -C   unshare cgroup namespace\n");
  fprintf(stderr, "    -i   unshare IPC namespace\n");
  fprintf(stderr, "    -m   unshare mount namespace\n");
  fprintf(stderr, "    -n   unshare network namespace\n");
  fprintf(stderr, "    -p   unshare PID namespace\n");
  fprintf(stderr, "    -t   unshare time namespace\n");
  fprintf(stderr, "    -u   unshare UTS namespace\n");
  fprintf(stderr, "    -U   unshare user namespace\n");
  exit(EXIT_FAILURE);
}

int
main(int argc, char *argv[])
{
  int flags, opt;

  flags = 0;

  while ((opt = getopt(argc, argv, "CimnptuU")) != -1) {
    switch (opt) {
    case 'C': flags |= CLONE_NEWCGROUP;      break;
    case 'i': flags |= CLONE_NEWIPC;        break;
    case 'm': flags |= CLONE_NEWNS;         break;
    case 'n': flags |= CLONE_NEWNET;        break;
    case 'p': flags |= CLONE_NEWPID;        break;
    case 't': flags |= CLONE_NEWTIME;        break;
    case 'u': flags |= CLONE_NEWUTS;        break;
    case 'U': flags |= CLONE_NEWUSER;       break;
    default:  usage(argv[0]);
    }
  }

  if (optind >= argc)
    usage(argv[0]);

  if (unshare(flags) == -1)
    err(EXIT_FAILURE, "unshare");

  execvp(argv[optind], &argv[optind]);
  err(EXIT_FAILURE, "execvp");
}
