#define _GNU_SOURCE
#include <errno.h>
#include <grp.h>
#include <pwd.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/stat.h>
#include <unistd.h>

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

int display_perms(char *fn) {
	struct stat sb;
	if (lstat(fn, &sb) < 0) {
		perror("Could not lstat");
		exit(1);
	}

	printf("%s: uid=%d, gid=%d\n", fn, sb.st_uid, sb.st_gid);

	return 0;
}
