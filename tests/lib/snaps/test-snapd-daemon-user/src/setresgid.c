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
		fprintf(stderr, "Usage: %s <rgroup> <egroup> <sgroup>\n", argv[0]);
		exit(EXIT_FAILURE);
	}

	gid_t gids[3];
	struct group *grps[3];

	for (int i=1; i<argc; i++) {
		if (strcmp(argv[i], "-1") == 0) {
			gids[i-1] = -1;
		} else {
			grps[i-1] = getgrnam(argv[i]);
			if (grps[i-1] == NULL) {
				printf("'%s' not found\n", argv[i]);
				exit(EXIT_FAILURE);
			}
			gids[i-1] = grps[i-1]->gr_gid;
		}
	}

	printf("Before: ");
	display();

	if (setresgid(gids[0], gids[1], gids[2]) < 0) {
		perror("setresgid");
		goto fail;
	}

	printf("After: ");
	display();

	exit(0);

 fail:
	exit(EXIT_FAILURE);
}
