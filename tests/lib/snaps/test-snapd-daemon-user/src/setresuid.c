#define _GNU_SOURCE
#include <errno.h>
#include <pwd.h>
#include <grp.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <string.h>

#include "display.h"

int main(int argc, char *argv[])
{
	if (argc < 4) {
		fprintf(stderr, "Usage: %s <ruser> <euser> <suser>\n", argv[0]);
		exit(EXIT_FAILURE);
	}

	uid_t uids[3];
	struct passwd *pwds[3];

	for (int i=1; i<argc; i++) {
		if (strcmp(argv[i], "-1") == 0) {
			uids[i-1] = -1;
		} else {
			pwds[i-1] = getpwnam(argv[i]);
			if (pwds[i-1] == NULL) {
				printf("'%s' not found\n", argv[i]);
				exit(EXIT_FAILURE);
			}
			uids[i-1] = pwds[i-1]->pw_uid;
		}
	}

	printf("Before: ");
	display();

	if (setresuid(uids[0], uids[1], uids[2]) < 0) {
		perror("setreuid");
		goto fail;
	}

	printf("After: ");
	display();

	exit(0);

 fail:
	exit(EXIT_FAILURE);
}
