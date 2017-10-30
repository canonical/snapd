#include "config.h"
#include "classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"

#include <string.h>
#include <stdio.h>
#include <unistd.h>

char *os_release = "/etc/os-release";

bool is_running_on_classic_distribution()
{
	FILE *f SC_CLEANUP(sc_cleanup_file) = fopen(os_release, "r");
	if (f == NULL) {
		return true;
	}

	char buf[255] = { 0 };
	while (fgets(buf, sizeof buf, f) != NULL) {
		if (strcmp(buf, "ID=ubuntu-core\n") == 0) {
			return false;
		}
	}
	return true;
}
