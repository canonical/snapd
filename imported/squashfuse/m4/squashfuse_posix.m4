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

# SQ_CHECK_NONSTD(NAME, PROLOG, SOURCE, [IF_NOT_FOUND])
#
# Check what #define we need for non-POSIX features.
# If no #define works and IF_NOT_FOUND is not given, exit on failure.
AC_DEFUN([SQ_CHECK_NONSTD],[
AH_TEMPLATE(AS_TR_CPP([NONSTD_]$1[_DEF]),
	[Extra definition needed by non-POSIX ]$1)

AS_VAR_PUSHDEF([sq_cache],[sq_cv_decl_nonstd_$1])
sq_msg="definition needed by $1"
AS_VAR_SET_IF([sq_cache],[
	AC_CACHE_CHECK([$sq_msg], [sq_cache])
],[
	AC_MSG_NOTICE([checking for $sq_msg])
	for sq_def in none _DARWIN_C_SOURCE _NETBSD_SOURCE _XOPEN_SOURCE \
		_BSD_SOURCE _GNU_SOURCE _POSIX_C_SOURCE
	do
		AS_IF([test "x$sq_def" = "x_POSIX_C_SOURCE"],[op=undef],[op=define])
		AS_IF([test "x$sq_def" = "x_XOPEN_SOURCE"],[val=500],[val=1])
		AS_IF([test "x$sq_def" = "xnone"],[
			sq_prolog=
			AC_MSG_CHECKING([if $1 works without changes])
		],[
			sq_prolog="#$op $sq_def $val"
			AC_MSG_CHECKING([if $1 requires changing $sq_def])
		])
		AC_LINK_IFELSE([AC_LANG_PROGRAM([
				$sq_prolog
				$2
			],[$3])
		],[
			AC_MSG_RESULT([yes])
			AS_VAR_SET([sq_cache],[$sq_def])
			break
		],[AC_MSG_RESULT([no])])
	done
])
AS_VAR_IF([sq_cache],[unknown],
	m4_default($4,[AC_MSG_FAILURE([can't figure out how to use $1])]),
	[AS_VAR_IF([sq_cache],[none],[],[
		AC_DEFINE_UNQUOTED(AS_TR_CPP([NONSTD_$1_DEF]),[CHANGE$sq_cache],[])
	])]
)
AS_VAR_POPDEF([sq_cache])
])

# SQ_CHECK_DECL_MAKEDEV_QNX([IF_NOT_FOUND])
#
# Check for QNX-style three argument makedev()
AC_DEFUN([SQ_CHECK_DECL_MAKEDEV_QNX],[
AC_CACHE_CHECK([for QNX makedev], [sq_cv_decl_makedev_qnx],[
	sq_cv_decl_makedev_qnx=no
	AC_LINK_IFELSE([AC_LANG_PROGRAM([
		#include <sys/types.h>
		#include <sys/sysmacros.h>
		#include <sys/netmgr.h>
		],[makedev(ND_LOCAL_NODE,0,0)])],
	[sq_cv_decl_makedev_qnx=yes])
])
AS_IF([test "x$sq_cv_decl_makedev_qnx" = xyes],
	[AC_DEFINE([QNX_MAKEDEV],[1],[define if makedev() is QNX-style])],
	[$1])
])

# Various feature checks
#
# SQ_CHECK_DECL_MAKEDEV		- makedev() macro
# SQ_CHECK_DECL_PREAD		- pread() in unistd.h
# SQ_CHECK_DECL_S_IFSOCK	- S_IFSOCK in struct stat mode
# SQ_CHECK_DECL_ENOATTR([IF_NOT_FOUND]) - ENOATTR error code
# SQ_CHECK_DECL_DAEMON		- daemon() in unistd.h

AC_DEFUN([SQ_CHECK_DECL_MAKEDEV],[
SQ_CHECK_DECL_MAKEDEV_QNX([
	AC_CHECK_HEADERS([sys/mkdev.h],,,[#include <sys/types.h>])
	AC_CHECK_HEADERS([sys/sysmacros.h],,,[#include <sys/types.h>])
	SQ_CHECK_NONSTD(makedev,[
		#include <sys/types.h>
		#ifdef HAVE_SYS_MKDEV_H
			#include <sys/mkdev.h>
		#endif
		#ifdef HAVE_SYS_SYSMACROS_H
			#include <sys/sysmacros.h>
		#endif
	],[makedev(0,0)])
])
])

AC_DEFUN([SQ_CHECK_DECL_PREAD],
	[SQ_CHECK_NONSTD(pread,[#include <unistd.h>],[(void)pread;])])
	
AC_DEFUN([SQ_CHECK_DECL_S_IFSOCK],
	[SQ_CHECK_NONSTD(S_IFSOCK,[#include <sys/stat.h>],[mode_t m = S_IFSOCK;])])

AC_DEFUN([SQ_CHECK_DECL_ENOATTR],[
AC_CHECK_HEADERS([attr/xattr.h],,,[#include <sys/types.h>])
SQ_CHECK_NONSTD(ENOATTR,[
	#ifdef HAVE_ATTR_XATTR_H
		#include <sys/types.h>
		#include <attr/xattr.h>
	#endif
	#include <errno.h>
],[int e = ENOATTR;],[$1])
])

AC_DEFUN([SQ_CHECK_DECL_DAEMON],
	[SQ_CHECK_NONSTD(daemon,[#include <unistd.h>],[(void)daemon;])])
