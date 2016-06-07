/*
 * Copyright (C) 2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */
#include "config.h"
#include "snap.h"

#include <stddef.h>
#include <stdlib.h>
#include <regex.h>

#include "utils.h"

bool verify_appname(const char *appname)
{
	// snappy appname is of form:
	// snap.<name>.<app>
	// - <name> must start with lowercase letter, then may contain
	//   lowercase alphanumerics and '-'
	// - <app> may contain alphanumerics and '-'
	const char *whitelist_re = "^snap\\.[a-z][a-z0-9-]*\\.[a-zA-Z0-9-]+$";
	regex_t re;
	if (regcomp(&re, whitelist_re, REG_EXTENDED | REG_NOSUB) != 0)
		die("can not compile regex %s", whitelist_re);

	int status = regexec(&re, appname, 0, NULL, 0);
	regfree(&re);

	return (status == 0);
}
