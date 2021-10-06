#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/time.h>
#include <sys/resource.h>

int main(int argc, char *argv[])
{
	int niceness = 10;
	if (argc >= 2) {
		niceness = atoi(argv[1]);
	}

	errno = 0;
	int rc = setpriority(PRIO_PROCESS, 0, niceness);
	if (rc != 0) {
		if (errno == EACCES) {
			// With the PRIO_PROCESS invocation, EACCES is a lack
			// of CAP_SYS_NICE which, if the syscall is allowed,
			// could non-root with negative nice value or LSM
			// denial.
			printf("Insufficient privileges (EACCES)\n");
		} else if (errno == EPERM) {
			// With the PRIO_PROCESS invocation, EPERM is only
			// possible with seccomp ERRNO(EPERM)
			printf("Operation not permitted (EPERM)\n");
		} else {
			perror("Other setpriority error");
		}
		return 1;
	}

	printf("Successfully used setpriority(PRIO_PROCESS, 0, %d)\n", niceness);
	return 0;
}
