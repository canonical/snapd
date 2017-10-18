/*
 * Copyright (c) 2014 Dave Vasilevsky <dave@vasilevsky.ca>
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
#include "util.h"

#include "fs.h"

#include <stdio.h>

#ifdef _WIN32
	#include <win32.h>
	
	sqfs_err sqfs_fd_open(const char *path, sqfs_fd_t *fd, bool print) {
		*fd = CreateFileA(path, GENERIC_READ, FILE_SHARE_READ, NULL, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, NULL);
		if (*fd != INVALID_HANDLE_VALUE)
			return SQFS_OK;

		// FIXME: Better error handling
		if (print)
			fprintf(stderr, "CreateFile error: %d\n", GetLastError());
		return SQFS_ERR;
	}

	void sqfs_fd_close(sqfs_fd_t fd) {
		CloseHandle(fd);
	}
#else
	#include <fcntl.h>
	#include <unistd.h>

	sqfs_err sqfs_fd_open(const char *path, sqfs_fd_t *fd, bool print) {
		*fd = open(path, O_RDONLY);
		if (*fd != -1)
			return SQFS_OK;

		if (print)
			perror("Can't open squashfs image");
		return SQFS_ERR;
	}

	void sqfs_fd_close(sqfs_fd_t fd) {
		close(fd);
	}
#endif


/* TODO: WIN32 implementation of open/close */
/* TODO: i18n of error messages */
sqfs_err sqfs_open_image(sqfs *fs, const char *image, size_t offset) {
	sqfs_err err;
	sqfs_fd_t fd;

	if ((err = sqfs_fd_open(image, &fd, stderr)))
		return err;

	err = sqfs_init(fs, fd, offset);
	switch (err) {
		case SQFS_OK:
			break;
		case SQFS_BADFORMAT:
			fprintf(stderr, "This doesn't look like a squashfs image.\n");
			break;
		case SQFS_BADVERSION: {
			int major, minor, mj1, mn1, mj2, mn2;
			sqfs_version(fs, &major, &minor);
			sqfs_version_supported(&mj1, &mn1, &mj2, &mn2);
			fprintf(stderr, "Squashfs version %d.%d detected, only version",
				major, minor);
			if (mj1 == mj2 && mn1 == mn2)
				fprintf(stderr, " %d.%d", mj1, mn1);
			else
				fprintf(stderr, "s %d.%d to %d.%d", mj1, mn1, mj2, mn2);
			fprintf(stderr, " supported.\n");
			break;
		}
		case SQFS_BADCOMP: {
			bool first = true;
			int i;
			sqfs_compression_type sup[SQFS_COMP_MAX],
				comp = sqfs_compression(fs);
			sqfs_compression_supported(sup);
			fprintf(stderr, "Squashfs image uses %s compression, this version "
				"supports only ", sqfs_compression_name(comp));
			for (i = 0; i < SQFS_COMP_MAX; ++i) {
				if (sup[i] == SQFS_COMP_UNKNOWN)
					continue;
				if (!first)
					fprintf(stderr, ", ");
				fprintf(stderr, "%s", sqfs_compression_name(sup[i]));
				first = false;
			}
			fprintf(stderr, ".\n");
			break;
		}
		default:
			fprintf(stderr, "Something went wrong trying to read the squashfs "
				"image.\n");
	}

	if (err)
		sqfs_fd_close(fd);
	return err;
}

