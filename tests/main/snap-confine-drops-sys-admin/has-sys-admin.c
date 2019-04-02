/*
 * gcc ./has-sys-admin.c -lcap -o has-sys-admin
 */

#include <stdio.h>
#include <stdlib.h>
#include <stdbool.h>
#include <errno.h>
#include <sys/capability.h>

bool has_cap(const char *s) {
	int res;
	bool rc = false;
	errno = 0;

	cap_t caps;
	cap_value_t cap;
	cap_flag_value_t cap_flags_value;

	if (cap_from_name(s, &cap) < 0) {
		fprintf(stderr, "could not cap_from_name()\n");
		exit(2);
	}

	if ((caps = cap_get_proc()) == NULL) {
		perror("could not get capabilities");
		exit(2);
	}

	res = cap_get_flag(caps, cap, CAP_EFFECTIVE, &cap_flags_value);
	cap_free(caps);
	if (res < 0) {
		fprintf(stderr, "could not cap_get_flag()\n");
		exit(2);
	}

	if (cap_flags_value == CAP_SET)
		rc = true;

	return rc;
}


int main(void)
{
	char *cap = "cap_sys_admin";
	if (has_cap(cap))
		printf("Can %s\n", cap);
	else
		printf("Cannot %s\n", cap);
	exit(0);
}
