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
#include "config.h"

#ifdef _WIN32
	#include "win32.h"

	ssize_t sqfs_pread(HANDLE file, void *buf, size_t count, sqfs_off_t off) {
		DWORD bread;
		OVERLAPPED ov = { 0 };
		ov.Offset = (DWORD)off;
		ov.OffsetHigh = (DWORD)(off >> 32);

		if (ReadFile(file, buf, count, &bread, &ov) == FALSE)
			return -1;
		return bread;
	}
#else
	#define SQFEATURE NONSTD_PREAD_DEF
	#include "nonstd-internal.h"

	#include <unistd.h>

	#include "common.h"

	ssize_t sqfs_pread(sqfs_fd_t fd, void *buf, size_t count, sqfs_off_t off) {
		return pread(fd, buf, count, off);
	}
#endif
