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
#ifndef CORE_LAUNCHER_UTILS_H
#define CORE_LAUNCHER_UTILS_H

#include <stdarg.h>
#include <stdbool.h>
#include <stdlib.h>

__attribute__((noreturn))
    __attribute__((format(printf, 1, 2)))
void die(const char *fmt, ...);

__attribute__((format(printf, 1, 2)))
void debug(const char *fmt, ...);

/**
 * sc_explain prints additional explanatory output to stdout.
 *
 * This function has effects only if explain mode is in effect.
 * See sc_is_explain_enabled for details.
 **/
__attribute__((format(printf, 1, 2)))
void sc_explain(const char *fmt, ...);

/**
 * sc_explain_start_section starts a new section with an (optional)
 * section description.
 *
 * This function has effects only if explain mode is in effect.
 **/
void sc_explain_start_section(const char *fmt, ...);

/**
 * sc_explain_end_section ends the current section *
 **/
void sc_explain_end_section(void);

/**
 * sc_explain_li writes out a new list-item
 **/
__attribute__((format(printf, 1, 2)))
void sc_explain_li(const char *fmt, ...);

/**
 * sc_explain_header prints an explain-style header to stdout.
 *
 * This function has effects only if explain mode is in effect.
 * See sc_is_explain_enabled for details.
 **/
void sc_explain_header(const char *name);

/**
 * Return true if debugging is enabled.
 *
 * This can used to avoid costly computation that is only useful for debugging.
 **/
bool sc_is_debug_enabled(void);

/**
 * Return true if re-execution is enabled.
 **/
bool sc_is_reexec_enabled(void);

/**
 * sc_is_explain_enabled returns true if snap run --explain mode is enabled.
 **/
bool sc_is_explain_enabled(void);

void write_string_to_file(const char *filepath, const char *buf);

/**
 * Safely create a given directory.
 *
 * NOTE: non-fatal functions don't die on errors. It is the responsibility of
 * the caller to call die() or handle the error appropriately.
 *
 * This function behaves like "mkdir -p" (recursive mkdir) with the exception
 * that each directory is carefully created in a way that avoids symlink
 * attacks. The preceding directory is kept openat(2) (along with O_DIRECTORY)
 * and the next directory is created using mkdirat(2), this sequence continues
 * while there are more directories to process.
 *
 * The function returns -1 in case of any error.
 **/
__attribute__((warn_unused_result))
int sc_nonfatal_mkpath(const char *const path, mode_t mode);
#endif
