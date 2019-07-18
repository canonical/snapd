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
		fprintf(stderr, "Usage: %s <user>\n", argv[0]);
		exit(EXIT_FAILURE);
	}

	struct passwd *pwd = getpwnam(argv[1]);
	if (pwd == NULL) {
		printf("'%s' not found\n", argv[1]);
		exit(EXIT_FAILURE);
	}

	printf("Before: ");
	display();

	if (setuid(pwd->pw_uid) < 0) {
		perror("setuid");
		goto fail;
	}

	printf("After: ");
	display();

	exit(0);

 fail:
	exit(EXIT_FAILURE);
}
