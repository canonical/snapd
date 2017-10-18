/*
 * Copyright (c) 2012 Dave Vasilevsky <dave@vasilevsky.ca>
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR(S) ``AS IS'' AND ANY EXPRESS OR
 * IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES
 * OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.
 * IN NO EVENT SHALL THE AUTHOR(S) BE LIABLE FOR ANY DIRECT, INDIRECT,
 * INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT
 * NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF
 * THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */
#define CHANGE_XOPEN_SOURCE 1
#define CHANGE_DARWIN_C_SOURCE 2
#define CHANGE_BSD_SOURCE 3
#define CHANGE_GNU_SOURCE 4
#define CHANGE_POSIX_C_SOURCE 5
#define CHANGE_NETBSD_SOURCE 6

#if SQFEATURE == CHANGE_XOPEN_SOURCE
	#define _XOPEN_SOURCE 500
#elif SQFEATURE == CHANGE_NETBSD_SOURCE
	#define _NETBSD_SOURCE
#elif SQFEATURE == CHANGE_DARWIN_C_SOURCE
	#define _DARWIN_C_SOURCE
#elif SQFEATURE == CHANGE_BSD_SOURCE
	#define _BSD_SOURCE
#elif SQFEATURE == CHANGE_GNU_SOURCE
	#define _GNU_SOURCE
#elif SQFEATURE == CHANGE_POSIX_C_SOURCE
	#undef _POSIX_C_SOURCE
#endif
