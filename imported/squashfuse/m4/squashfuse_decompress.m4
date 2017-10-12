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

# SQ_CHECK_DECOMPRESS(NAME, LIBRARY, FUNCTION, HEADER, [PKGCONFIG])
#
# Check for a decompression library with the given library name, function and
# header. If given pkg-config package name, also look using pkg-config.
#
# On success modify CPPFLAGS and LIBS and append NAME to sq_decompressors.
AC_DEFUN([SQ_CHECK_DECOMPRESS],[
	SQ_SAVE_FLAGS
	
	sq_want=yes
	sq_specified=no
	AC_ARG_WITH(m4_tolower($1),
		AS_HELP_STRING([--with-]m4_tolower($1)[=DIR],
			m4_tolower($1)[ prefix directory]),[
		AS_IF([test "x$withval" = xno],[
			sq_want=no
		],[
			sq_specified=yes
			CPPFLAGS="$CPPFLAGS -I$withval/include"
			LIBS="$LIBS -L$withval/lib"
		])
	])
	
	sq_dec_ok=
	AS_IF([test "x$sq_want" = xyes],[		
		sq_lib="$2"
		m4_ifval($5,[AS_IF([test "x$sq_specified" = xno],[
			SQ_PKG($1,$5,[sq_lib=],[:])
		])])
		
		sq_dec_ok=yes
		AC_SEARCH_LIBS($3,[$sq_lib],,[sq_dec_ok=])
		AS_IF([test "x$sq_dec_ok" = xyes],[AC_CHECK_HEADERS($4,,[sq_dec_ok=])])
		
		AS_IF([test "x$sq_dec_ok" = xyes],[
			sq_decompressors="$sq_decompressors $1"
		],[
			AS_IF([test "x$sq_specified" = xyes],
				[AC_MSG_FAILURE([Asked for ]$1[, but it can't be found])])
		])
	])
	SQ_KEEP_FLAGS($1,[$sq_dec_ok])
])
