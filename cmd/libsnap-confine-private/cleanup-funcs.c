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

#include "cleanup-funcs.h"

#include <mntent.h>
#include <unistd.h>

void sc_cleanup_string(char **ptr) {
    if (ptr != NULL && *ptr != NULL) {
        free(*ptr);
        *ptr = NULL;
    }
}

void sc_cleanup_deep_strv(char ***ptr) {
    if (ptr != NULL && *ptr != NULL) {
        for (char **str = *ptr; *str != NULL; str++) {
            free(*str);
        }
        free(*ptr);
        *ptr = NULL;
    }
}

void sc_cleanup_shallow_strv(const char ***ptr) {
    if (ptr != NULL && *ptr != NULL) {
        free(*ptr);
        *ptr = NULL;
    }
}

void sc_cleanup_file(FILE **ptr) {
    if (ptr != NULL && *ptr != NULL) {
        fclose(*ptr);
        *ptr = NULL;
    }
}

void sc_cleanup_endmntent(FILE **ptr) {
    if (ptr != NULL && *ptr != NULL) {
        endmntent(*ptr);
        *ptr = NULL;
    }
}

void sc_cleanup_closedir(DIR **ptr) {
    if (ptr != NULL && *ptr != NULL) {
        closedir(*ptr);
        *ptr = NULL;
    }
}

void sc_cleanup_close(int *ptr) {
    if (ptr != NULL && *ptr != -1) {
        close(*ptr);
        *ptr = -1;
    }
}
