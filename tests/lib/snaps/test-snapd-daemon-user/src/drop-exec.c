/*
 * gcc ./drop.c -o drop
 */

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
		fprintf(stderr, "Usage: %s <username> <exec>\n", argv[0]);
		exit(EXIT_FAILURE);
	}

	/* Convert our username to a passwd entry */
	struct passwd *pwd = getpwnam(argv[1]);
	if (pwd == NULL) {
		printf("'%s' not found\n", argv[1]);
		exit(EXIT_FAILURE);
	}

	printf("Before: ");
	display();

	// not portable outside of Linux, but snap-friendly
	if (setgroups(0, NULL) < 0) {
		perror("setgroups");
		goto fail;
	}

	/* Drop gid after supplementary groups */
	if (setgid(pwd->pw_gid) < 0) {
		perror("setgid");
		goto fail;
	}

	/* Drop uid after gid */
	if (setuid(pwd->pw_uid) < 0) {
		perror("setuid");
		goto fail;
	}

	printf("After: ");
	display();

	printf("Executing: %s...\n", argv[2]);
	execv(argv[2], (char *const *)&argv[2]);
	perror("execv failed");

 fail:
	exit(EXIT_FAILURE);
}
