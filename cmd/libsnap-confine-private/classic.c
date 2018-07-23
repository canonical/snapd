#include "config.h"
#include "classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/string-utils.h"

#include <stdbool.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

char *os_release = "/etc/os-release";

sc_distro sc_classify_distro(void)
{
	FILE *f SC_CLEANUP(sc_cleanup_file) = fopen(os_release, "r");
	if (f == NULL) {
		return SC_DISTRO_CLASSIC;
	}

	bool is_core = false;
	int core_version = 0;
	char buf[255] = { 0 };

	while (fgets(buf, sizeof buf, f) != NULL) {
		size_t len = strlen(buf);
		if (len > 0 && buf[len - 1] == '\n') {
			buf[len - 1] = '\0';
		}
		if (sc_streq(buf, "ID=\"ubuntu-core\"")
		    || sc_streq(buf, "ID=ubuntu-core")) {
			is_core = true;
		} else if (sc_streq(buf, "VERSION_ID=\"16\"")
			   || sc_streq(buf, "VERSION_ID=16")) {
			core_version = 16;
		}
	}

	if (is_core) {
		if (core_version == 16) {
			return SC_DISTRO_CORE16;
		}
		return SC_DISTRO_CORE_OTHER;
	} else {
		return SC_DISTRO_CLASSIC;
	}
}

bool sc_should_use_normal_mode(sc_distro distro, const char *base_snap_name)
{
	return distro != SC_DISTRO_CORE16 || !sc_streq(base_snap_name, "core");
}
