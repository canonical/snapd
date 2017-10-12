# Copyright (c) 2012 Dave Vasilevsky <dave@vasilevsky.ca>
# All rights reserved.
# 
# Redistribution and use in source and binary forms, with or without
# modification, are permitted provided that the following conditions
# are met:
# 1. Redistributions of source code must retain the above copyright
#    notice, this list of conditions and the following disclaimer.
# 2. Redistributions in binary form must reproduce the above copyright
#    notice, this list of conditions and the following disclaimer in the
#    documentation and/or other materials provided with the distribution.
# 
# THIS SOFTWARE IS PROVIDED BY THE AUTHOR(S) ``AS IS'' AND ANY EXPRESS OR
# IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES
# OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.
# IN NO EVENT SHALL THE AUTHOR(S) BE LIABLE FOR ANY DIRECT, INDIRECT,
# INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT
# NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
# DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
# THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
# (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF
# THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

# SQ_PROG_CPP_POSIX_2001
#
# Check if a preprocessor flag is needed for POSIX-2001 headers.
# Needed at least on Solaris and derivatives.
AC_DEFUN([SQ_PROG_CPP_POSIX_2001],[
AC_CACHE_CHECK([for option for POSIX-2001 preprocessor], 
	[sq_cv_prog_cpp_posix2001],
[
	sq_cv_prog_cpp_posix2001=unknown
	sq_save_CPPFLAGS=$CPPFLAGS
	for sq_flags in none -std=gnu99 -xc99=all
	do
		AS_IF([test "x$sq_flags" = xnone],,
			[CPPFLAGS="$save_CPPFLAGS $sq_flags"])
		AC_PREPROC_IFELSE([AC_LANG_PROGRAM([
			#define _POSIX_C_SOURCE 200112L
			#include <sys/types.h>
		])],[
			sq_cv_prog_cpp_posix2001=$sq_flags
			break
		])
	done
	CPPFLAGS=$sq_save_CPPFLAGS
])
AS_IF([test "x$sq_cv_prog_cpp_posix2001" = xunknown],
	[AC_MSG_FAILURE([can't preprocess for POSIX-2001])],
	[AS_IF([test "x$sq_cv_prog_cpp_posix2001" = xnone],,
		CPPFLAGS="$CPPFLAGS $sq_cv_prog_cpp_posix2001")
])
])

# SQ_PROG_CC_WALL
#
# Check if -Wall is supported
AC_DEFUN([SQ_PROG_CC_WALL],[
AC_CACHE_CHECK([how to enable all compiler warnings],
	[sq_cv_prog_cc_wall],
[
	sq_cv_prog_cc_wall=unknown
	sq_save_CFLAGS=$CFLAGS
	CFLAGS="$CFLAGS -Wall"
	AC_COMPILE_IFELSE([AC_LANG_PROGRAM(,)],[sq_cv_prog_cc_wall="-Wall"])
	CFLAGS=$sq_save_CFLAGS
])
AS_IF([test "x$sq_cv_prog_cc_wall" = xunknown],,
	[AC_SUBST([AM_CFLAGS],["$AM_CFLAGS $sq_cv_prog_cc_wall"])])
])