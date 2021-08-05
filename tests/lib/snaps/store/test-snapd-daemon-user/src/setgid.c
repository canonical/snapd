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
	if (argc < 2) {
		fprintf(stderr, "Usage: %s <group>\n", argv[0]);
		exit(EXIT_FAILURE);
	}

	struct group *grp = getgrnam(argv[1]);
	if (grp == NULL) {
		printf("'%s' not found\n", argv[1]);
		exit(EXIT_FAILURE);
	}

	printf("Before: ");
	display();

	if (setgid(grp->gr_gid) < 0) {
		perror("setgid");
		goto fail;
	}

	printf("After: ");
	display();

	exit(0);

 fail:
	exit(EXIT_FAILURE);
}
