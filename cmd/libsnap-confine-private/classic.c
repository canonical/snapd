#include "config.h"
#include "classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"

#include <string.h>
#include <stdio.h>
#include <unistd.h>

char *os_release = "/etc/os-release";

bool is_running_on_classic_distribution()
{
	char buf[255];
	int is_core = false;

	FILE *f SC_CLEANUP(sc_cleanup_file) = fopen(os_release, "r");
	if (f == NULL) {
		return !is_core;
	}
	while (fgets(buf, sizeof buf, f) != NULL) {
		if (strcmp(buf, "ID=ubuntu-core\n") == 0) {
			is_core = true;
			break;
		}
	}
	return !is_core;
}
