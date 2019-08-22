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
	if (argc < 3) {
		fprintf(stderr, "Usage: %s <rgroup> <egroup>\n", argv[0]);
		exit(EXIT_FAILURE);
	}

	gid_t uids[2];
	struct group *grps[2];

	for (int i=1; i<argc; i++) {
		if (strcmp(argv[i], "-1") == 0) {
			uids[i-1] = -1;
		} else {
			grps[i-1] = getgrnam(argv[i]);
			if (grps[i-1] == NULL) {
				printf("'%s' not found\n", argv[i]);
				exit(EXIT_FAILURE);
			}
			uids[i-1] = grps[i-1]->gr_gid;
		}
	}

	printf("Before: ");
	display();

	if (setregid(uids[0], uids[1]) < 0) {
		perror("setregid");
		goto fail;
	}

	printf("After: ");
	display();

	exit(0);

 fail:
	exit(EXIT_FAILURE);
}
