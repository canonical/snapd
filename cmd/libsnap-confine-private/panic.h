/*
 * Copyright (C) 2019 Canonical Ltd
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

#ifndef SC_PANIC_H
#define SC_PANIC_H

#include <stdarg.h>

/**
 * sc_panic is an exit-with-message utility function.
 *
 * The function takes a printf-like format string that is formatted and printed
 * somehow. The function then terminates the process by calling exit. Both
 * aspects can be customized.
 *
 * The particular nature of the exit can be customized by calling
 * sc_set_panic_action. The panic action is a function that is called before
 * attempting to exit.
 *
 * The way the error message is formatted and printed can be customized by
 * calling sc_set_panic_format_fn(). By default the error is printed to
 * standard error. If the error is related to a system call failure then errno
 * can be set to a non-zero value just prior to calling sc_panic. The value
 * will then be used when crafting the error message.
 **/
__attribute__((noreturn, format(printf, 1, 2))) void sc_panic(const char *fmt, ...);

/**
 * sc_panicv is a variant of sc_panic with an argument list.
 **/
__attribute__((noreturn)) void sc_panicv(const char *fmt, va_list ap);

/**
 * sc_panic_exit_fn is the type of the exit function used by sc_panic().
 **/
typedef void (*sc_panic_exit_fn)(void);

/**
 * sc_set_panic_exit_fn sets the panic exit function.
 *
 * When sc_panic is called it will eventually exit the running process. Just
 * prior to that, it will call the panic exit function, if one has been set.
 *
 * If exiting the process is undesired, for example while running in intrd as
 * pid 1, during the system shutdown phase, then a process can set the panic
 * exit function. Note that if the specified function returns then panic will
 * proceed to call exit(3) anyway.
 *
 * The old exit function, if any, is returned.
 **/
sc_panic_exit_fn sc_set_panic_exit_fn(sc_panic_exit_fn fn);

/**
 * sc_panic_msg_fn is the type of the format function used by sc_panic().
 **/
typedef void (*sc_panic_msg_fn)(const char *fmt, va_list ap, int errno_copy);

/**
 * sc_set_panic_msg_fn sets the panic message function.
 *
 * When sc_panic is called it will attempt to print an error message to
 * standard error. The message includes information provided by the caller: the
 * format string, the argument vector for a printf-like function as well as a
 * copy of the system errno value, which may be zero if the error is not
 * originated by a system call error.
 *
 * If custom formatting of the error message is desired, for example while
 * running in initrd as pid 1, during the system shutdown phase, then a process
 * can set the panic message function. Once set the function takes over the
 * responsibility of printing an error message (in whatever form is
 * appropriate).
 *
 * The old message function, if any, is returned.
 **/
sc_panic_msg_fn sc_set_panic_msg_fn(sc_panic_msg_fn fn);

#endif
