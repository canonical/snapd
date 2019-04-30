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
		fprintf(stderr, "Usage: %s <file> <user> <group>\n", argv[0]);
		exit(EXIT_FAILURE);
	}

	uid_t uid = -1;
	struct passwd *pwd;

	gid_t gid = -1;
	struct group *grp;

	if (strcmp(argv[2], "-1") != 0) {
		pwd = getpwnam(argv[2]);
		if (pwd == NULL) {
			printf("'%s' not found\n", argv[2]);
			exit(EXIT_FAILURE);
		}
		uid = pwd->pw_uid;
	}

	if (strcmp(argv[3], "-1") != 0) {
		grp = getgrnam(argv[3]);
		if (grp == NULL) {
			printf("'%s' not found\n", argv[3]);
			exit(EXIT_FAILURE);
		}
		gid = grp->gr_gid;
	}

	printf("Before: ");
	display_perms(argv[1]);

	if (lchown(argv[1], uid, gid) < 0) {
		perror("lchown");
		goto fail;
	}

	printf("After: ");
	display_perms(argv[1]);

	exit(0);

 fail:
	exit(EXIT_FAILURE);
}
