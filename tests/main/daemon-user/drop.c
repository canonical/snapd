/*
 * gcc ./drop.c -o drop
 */

#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <stdbool.h>
#include <errno.h>
#include <pwd.h>
#include <grp.h>
#include <unistd.h>
#include <sys/types.h>
#include <sys/capability.h>
#include <string.h>

int display(void) {
	int i;
	uid_t ruid, euid, suid;
	gid_t rgid, egid, sgid;
	gid_t *groups = NULL;
	int ngroups = 0;
	long ngroups_max = sysconf(_SC_NGROUPS_MAX) + 1;

	if (getresuid(&ruid, &euid, &suid) < 0) {
		perror("Could not getresuid");
		exit(1);
	}
	if (getresgid(&rgid, &egid, &sgid) < 0) {
		perror("Could not getresgid");
		exit(1);
	}

	/* Get our supplementary groups */
	groups = (gid_t *) malloc(ngroups_max * sizeof(gid_t));
	if (groups == NULL) {
		printf("Could not allocate memory\n");
		exit(EXIT_FAILURE);
	}
	ngroups = getgroups(ngroups_max, groups);
	if (ngroups < 0) {
		perror("getgroups");
		free(groups);
		exit(1);
	}

	/* Display dropped privileges */
	printf("ruid=%d, euid=%d, suid=%d, ", ruid, euid, suid);
	printf("rgid=%d, egid=%d, sgid=%d, ", rgid, egid, sgid);
	printf("groups=");
	for (i = 0; i < ngroups; i++) {
		printf("%d", groups[i]);
		if (i < ngroups - 1)
			printf(",");
	}
	printf("\n");

	free(groups);

	return 0;
}

int main(int argc, char *argv[])
{
	char *default_user = "daemon";
	char *user = NULL;
	struct passwd *pwd = NULL;

	if (argc > 1) {
		user = argv[1];
	} else {
		user = default_user;
	}
	/* Convert our username to a passwd entry */
	pwd = getpwnam(user);

	if (pwd == NULL) {
		printf("'%s' not found\n", user);
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

	exit(0);

 fail:
	exit(EXIT_FAILURE);
}
