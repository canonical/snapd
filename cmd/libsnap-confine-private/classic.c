#include "config.h"
#include "classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

#include <errno.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

static const char *os_release = "/etc/os-release";
static const char *meta_snap_yaml = "/meta/snap.yaml";

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
		} else if (sc_streq(buf, "VARIANT_ID=\"snappy\"")
			   || sc_streq(buf, "VARIANT_ID=snappy")) {
			is_core = true;
		}
	}

	if (!is_core) {
		/* Since classic systems don't have a /meta/snap.yaml file the simple
		   presence of that file qualifies as SC_DISTRO_CORE_OTHER. */
		if (access(meta_snap_yaml, F_OK) == 0) {
			is_core = true;
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

void sc_probe_distro(const char *os_release_path, ...)
{
	FILE *f SC_CLEANUP(sc_cleanup_file) = fopen(os_release_path, "r");
	if (f == NULL) {
		die("cannot open %s", os_release);
	}

	va_list ap;
	va_start(ap, os_release_path);

	size_t line_size = 0;
	char *line_buf SC_CLEANUP(sc_cleanup_string) = NULL;
	for (;;) {		/* This loop advances through the keys we are looking for. */
		const char *key = va_arg(ap, const char *);
		if (key == NULL) {
			break;
		}
		char **value = va_arg(ap, char **);
		if (value != NULL) {
			*value = NULL;
		}
		size_t key_len = strlen(key);

		fseek(f, 0, SEEK_SET);
		for (;;) {	/* This loop advances through subsequent lines. */
			ssize_t nread = getline(&line_buf, &line_size, f);
			if (nread < 0 && errno != 0) {
				die("cannot read another line");
			}
			if (nread <= 0) {
				break;	/* There is nothing more to read. */
			}
			/* Skip lines shorter than the key length. They cannot match our
			 * key. The extra byte ensures that we can look for the equals sign
			 * ('='). Note that at this time nread cannot be negative. */
			if ((size_t)nread < key_len + 1) {
				continue;
			}
			/* Replace the newline character, if any, with the NUL byte. */
			if (nread > 0 && line_buf[nread - 1] == '\n') {
				line_buf[nread - 1] = '\0';
			}
			/* If the prefix of the line is the search key followed by the
			 * equals sign then this is a matching entry. Copy it to the
			 * provided pointer, if any, and stop searching. */
			if (strstr(line_buf, key) == line_buf
			    && line_buf[key_len] == '=') {
				if (value != NULL) {
					*value =
					    sc_strdup(line_buf + key_len + 1);
				}
				break;
			}
		}
	}
	va_end(ap);
}
