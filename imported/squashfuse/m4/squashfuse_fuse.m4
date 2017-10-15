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

# SQ_TRY_FUSE(LIBS, [IF-FOUND], [IF-NOT-FOUND])
#
# Check if FUSE low-level compiles and links correctly.
AC_DEFUN([SQ_TRY_FUSE],[
	sq_fuse_ok=yes
	AS_VAR_PUSHDEF([sq_cv_lib],[sq_cv_lib_fuse_""$1""_""$LIBS])
	AC_CACHE_CHECK([for FUSE library],[sq_cv_lib],[
		for sq_lib in '' $1
		do
			SQ_SAVE_FLAGS
			AS_IF([test "x$sq_lib" = x],,[LIBS="$LIBS -l$sq_lib"])
			AC_LINK_IFELSE([AC_LANG_CALL(,[fuse_get_context])],[
				AS_IF([test "x$sq_lib" = x],[sq_lib_out="already present"],
					[sq_lib_out="-l$sq_lib"])
				AS_VAR_SET([sq_cv_lib],[$sq_lib_out])
			])
			SQ_RESTORE_FLAGS
			AS_VAR_SET_IF([sq_cv_lib],[break])
		done
		AS_VAR_SET_IF([sq_cv_lib],,[AS_VAR_SET([sq_cv_lib],[no])])
	])
	AS_VAR_IF([sq_cv_lib],[no],[sq_fuse_ok=no])
	
	AS_IF([test "x$sq_fuse_ok" = "xno"],,[
		AS_VAR_PUSHDEF([sq_cv_hdr],[sq_cv_header_fuse_""$CPPFLAGS])
		AC_CACHE_CHECK([for FUSE header],[sq_cv_hdr],[
			AC_COMPILE_IFELSE([AC_LANG_PROGRAM([
					#include <fuse.h>
					#include <fuse_opt.h>
				])],
				[AS_VAR_SET([sq_cv_hdr],[yes])],
				[AS_VAR_SET([sq_cv_hdr],[no])]
			)
		])
		AS_VAR_IF([sq_cv_hdr],[yes],,[sq_fuse_ok=no])
		AS_VAR_POPDEF([sq_cv_hdr])
	])
	
	AS_IF([test "x$sq_fuse_ok" = "xno"],[$3],[
		AS_VAR_COPY([sq_lib_res],[sq_cv_lib])
		AS_IF([test "x$sq_lib_res" = "xalready present"],,
			[LIBS="$LIBS $sq_lib_res"])
		$2
	])
	AS_VAR_POPDEF([sq_cv_lib])
])

# SQ_TRY_FUSE_DIRS(NAME, INCDIR, LIBDIR, CPPFLAGS, LIBS,
#	[IF-FOUND], [IF-NOT-FOUND])
#
# Check if FUSE is in any of the given directories
AC_DEFUN([SQ_TRY_FUSE_DIRS],[
	AS_IF([test "x$sq_fuse_found" = xyes],,[
		AS_IF([test "x$1" = x],,[AC_MSG_NOTICE([Checking for FUSE in $1])])
		
		SQ_SAVE_FLAGS
		AS_IF([test "x$2" = x],,[CPPFLAGS="$CPPFLAGS -I$2"])
		AS_IF([test "x$3" = x],,[LIBS="$LIBS -L$3"])
		CPPFLAGS="$CPPFLAGS $4"
		SQ_TRY_FUSE($5,[sq_fuse_found=yes],[sq_fuse_found=])
		SQ_KEEP_FLAGS([FUSE],[$sq_fuse_found])
		AS_IF([test "x$sq_fuse_found" = xyes],$6,$7)
	])
])

# SQ_SEARCH_FUSE_DIRS
#
# Nobody told us where FUSE is, search some common places.
AC_DEFUN([SQ_SEARCH_FUSE_DIRS],[
	AS_CASE([$target_os],[darwin*],[
		SQ_TRY_FUSE_DIRS([OSXFUSE directories],
			[/usr/local/include/osxfuse/fuse],[/usr/local/lib],
			[$sq_fuse_cppflags],[osxfuse $sq_fuse_libs])
	])
	SQ_TRY_FUSE_DIRS([default directories],,,
		[$sq_fuse_cppflags],[$sq_fuse_libs])
	SQ_TRY_FUSE_DIRS([/usr],[/usr/include/fuse],[/usr/lib],
		[$sq_fuse_cppflags],[$sq_fuse_libs])
	SQ_TRY_FUSE_DIRS([/usr/local],[/usr/local/include/fuse],[/usr/local/lib],
		[$sq_fuse_cppflags],[$sq_fuse_libs])
	
	AS_IF([test "x$sq_fuse_found" = xyes],[
		sq_cv_lib_fuse_LIBS="$FUSE_LIBS"
		sq_cv_lib_fuse_CPPFLAGS="$FUSE_CPPFLAGS"
	],[sq_cv_lib_fuse_LIBS=no])
])

# SQ_FIND_FUSE
#
# Find the FUSE library
AC_DEFUN([SQ_FIND_FUSE],[
	# FUSE headers usually demand _FILE_OFFSET_BITS=64
	sq_fuse_cppflags="-D_FILE_OFFSET_BITS=64"
	sq_fuse_libs="fuse"
	AS_CASE([$target_os],[darwin*],[
		sq_fuse_cppflags="$sq_fuse_cppflags -D__FreeBSD__=10 -D_DARWIN_USE_64_BIT_INODE"
		sq_fuse_libs="osxfuse fuse4x fuse_ino64 $sq_fuse_libs"
	])
	AC_ARG_WITH(fuse-soname,
		AS_HELP_STRING([--with-fuse-soname=SONAME], [FUSE library name]),
		[sq_fuse_libs="$withval"])
	sq_fuse_found=
	
	# Specified in arguments
	AC_ARG_WITH(fuse,
		AS_HELP_STRING([--with-fuse=DIR], [FUSE prefix directory]),[
		fuse_inc="$withval/include/fuse"
		fuse_lib="$withval/lib"
	])
	
	AC_ARG_WITH(fuse-include,
		AS_HELP_STRING([--with-fuse-include=DIR], [FUSE header directory]),
		[fuse_inc=$withval])
	AC_ARG_WITH(fuse-lib,
		AS_HELP_STRING([--with-fuse-lib=DIR], [FUSE library directory]),
		[fuse_lib=$withval])
	AS_IF([test "x$fuse_inc$fuse_lib" = x],,[
		SQ_TRY_FUSE_DIRS(,[$fuse_inc],[$fuse_lib],[$sq_fuse_cppflags],
			[$sq_fuse_libs],,
			[AC_MSG_FAILURE([Can't find FUSE in specified directories])])
	])
	
	# pkgconfig
	AS_IF([test "x$sq_fuse_found" = xyes],,[
		SQ_SAVE_FLAGS
		SQ_PKG([fuse],[fuse >= 2.5],
			[SQ_TRY_FUSE(,[sq_fuse_found=yes],
				[AC_MSG_FAILURE([Can't find FUSE with pkgconfig])])],
			[:])
		SQ_KEEP_FLAGS([FUSE],[$sq_fuse_found])
	])
	
	# Default search locations
	AS_IF([test "x$sq_cv_lib_fuse_LIBS" = x],[SQ_SEARCH_FUSE_DIRS],[
		AS_IF([test "x$sq_cv_lib_fuse_LIBS" = xno],,[
			AC_CACHE_CHECK([FUSE libraries],[sq_cv_lib_fuse_LIBS])
			AC_CACHE_CHECK([FUSE preprocessor flags],[sq_cv_lib_fuse_CPPFLAGS])
			FUSE_LIBS=$sq_cv_lib_fuse_LIBS
			FUSE_CPPFLAGS=$sq_cv_lib_fuse_CPPFLAGS
			sq_fuse_found=yes
		])
	])
	
	AS_IF([test "x$sq_fuse_found" = xyes],,
		[AC_MSG_FAILURE([Can't find FUSE])])
])

# SQ_FUSE_API
#
# Check for the high-level FUSE API
AC_DEFUN([SQ_FUSE_API],[
	AC_ARG_ENABLE([high-level],
		AS_HELP_STRING([--disable-high-level], [disable high-level FUSE driver]),,
		[enable_high_level=yes])
	AC_ARG_ENABLE([low-level],
		AS_HELP_STRING([--disable-low-level], [disable low-level FUSE driver]),,
		[enable_low_level=check])
	AC_ARG_ENABLE(fuse,
		AS_HELP_STRING([--disable-fuse], [disable all FUSE drivers]))
	AS_IF([test "x$enable_fuse" = xno],[
		enable_high_level=no
		enable_low_level=no
	])

	AS_IF([test "x$enable_high_level$enable_low_level" = xnono],,[SQ_FIND_FUSE])
])

# SQ_FUSE_API_LOWLEVEL
#
# Check if we have the low-level FUSE API available
AC_DEFUN([SQ_FUSE_API_LOWLEVEL],[
	AS_IF([test "x$enable_low_level" = xno],,[
		SQ_SAVE_FLAGS
		LIBS="$LIBS $FUSE_LIBS"
		CPPFLAGS="$CPPFLAGS $FUSE_CPPFLAGS"
	
		sq_fuse_lowlevel_found=yes
		AC_CHECK_DECL([fuse_lowlevel_new],,[sq_fuse_lowlevel_found=no],
			[#include <fuse_lowlevel.h>])
		AC_CHECK_FUNC([fuse_lowlevel_new],,[sq_fuse_lowlevel_found=no])
	
		SQ_RESTORE_FLAGS
		
		AS_IF([test "x$sq_fuse_lowlevel_found" = xno],[
			sq_err="The low-level FUSE API is not available"
			AS_IF([test "x$enable_low_level" = xyes],[AC_MSG_FAILURE($sq_err)],
				[AC_MSG_WARN($sq_err)])
		])
		enable_low_level="$sq_fuse_lowlevel_found"
	])
])

# SQ_FUSE_RESULT
#
# Handle the results of FUSE checks
AC_DEFUN([SQ_FUSE_RESULT],[
	AS_IF([test "x$enable_high_level$enable_low_level" = xnono],[
		AC_MSG_WARN([Without any FUSE support, you will not be able to mount squashfs archives])
	])
	AM_CONDITIONAL([SQ_WANT_HIGHLEVEL], [test "x$enable_high_level" = xyes])
	AM_CONDITIONAL([SQ_WANT_LOWLEVEL], [test "x$enable_low_level" = xyes])
])

# SQ_FUSE_API_VERSION
#
# Check various things that are different in different versions of FUSE
AC_DEFUN([SQ_FUSE_API_VERSION],[
	SQ_SAVE_FLAGS
	LIBS="$LIBS $FUSE_LIBS"
	CPPFLAGS="$CPPFLAGS $FUSE_CPPFLAGS"
	
	AS_IF([test "x$enable_low_level" = xyes],[
		AC_CHECK_DECLS([fuse_add_direntry,fuse_add_dirent],[found_dirent=yes],,
			[#include <fuse_lowlevel.h>])
		AS_IF([test "x$found_dirent" = xyes],,
			[AC_MSG_FAILURE([No way to add directory entries])])

		AC_CHECK_DECLS([fuse_daemonize],,
			[SQ_CHECK_NONSTD(daemon,[#include <unistd.h>],[(void)daemon;])],
			[#include <fuse_lowlevel.h>])

		AC_CHECK_DECLS([fuse_session_remove_chan],,,
			[#include <fuse_lowlevel.h>])
	
		AC_CACHE_CHECK([for two-argument fuse_unmount],
				[sq_cv_decl_fuse_unmount_two_arg],[
			AC_LINK_IFELSE(
				[AC_LANG_PROGRAM([#include <fuse_lowlevel.h>],
					[fuse_unmount(0,0)])],
				[sq_cv_decl_fuse_unmount_two_arg=yes],
				[sq_cv_decl_fuse_unmount_two_arg=no])
		])
		AS_IF([test "x$sq_cv_decl_fuse_unmount_two_arg" = xyes],[
			AC_DEFINE([HAVE_NEW_FUSE_UNMOUNT],1,
					[Define if we have two-argument fuse_unmount])
		])
	])
	
	SQ_RESTORE_FLAGS
])

# SQ_FUSE_API_XATTR_POSITION
#
# Check for OS X's special flag to getxattr.
AC_DEFUN([SQ_FUSE_API_XATTR_POSITION],[
	SQ_SAVE_FLAGS
	LIBS="$LIBS $FUSE_LIBS"
	CPPFLAGS="$CPPFLAGS $FUSE_CPPFLAGS"
	
	AC_CACHE_CHECK([for position argument to FUSE xattr operations],
		[sq_cv_decl_fuse_xattr_position],[
		AC_LINK_IFELSE([AC_LANG_PROGRAM([#include <fuse.h>],[
				struct fuse_operations ops;
				ops.getxattr(0, 0, 0, 0, 0);
			])],
			[sq_cv_decl_fuse_xattr_position=yes],
			[sq_cv_decl_fuse_xattr_position=no])
	])
	AS_IF([test "x$sq_cv_decl_fuse_xattr_position" = xyes],[
		AC_DEFINE([FUSE_XATTR_POSITION],1,
			[Define if FUSE xattr operations take a position argument])
	])
	
	SQ_RESTORE_FLAGS
])
