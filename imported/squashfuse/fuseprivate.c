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
#include "fuseprivate.h"

#include <ctype.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "nonstd.h"

sqfs_err sqfs_stat(sqfs *fs, sqfs_inode *inode, struct stat *st) {
	sqfs_err err = SQFS_OK;
	uid_t id;
	
	memset(st, 0, sizeof(*st));
	st->st_mode = inode->base.mode;
	st->st_nlink = inode->nlink;
	st->st_mtime = st->st_ctime = st->st_atime = inode->base.mtime;
	
	if (S_ISREG(st->st_mode)) {
		/* FIXME: do symlinks, dirs, etc have a size? */
		st->st_size = inode->xtra.reg.file_size;
		st->st_blocks = st->st_size / 512;
	} else if (S_ISBLK(st->st_mode) || S_ISCHR(st->st_mode)) {
		st->st_rdev = sqfs_makedev(inode->xtra.dev.major,
			inode->xtra.dev.minor);
	} else if (S_ISLNK(st->st_mode)) {
		st->st_size = inode->xtra.symlink_size;
	}
	
	st->st_blksize = fs->sb.block_size; /* seriously? */
	
	err = sqfs_id_get(fs, inode->base.uid, &id);
	if (err)
		return err;
	st->st_uid = id;
	err = sqfs_id_get(fs, inode->base.guid, &id);
	st->st_gid = id;
	if (err)
		return err;
	
	return SQFS_OK;
}

int sqfs_listxattr(sqfs *fs, sqfs_inode *inode, char *buf, size_t *size) {
	sqfs_xattr x;
	size_t count = 0;
	
	if (sqfs_xattr_open(fs, inode, &x))
		return -EIO;
	
	while (x.remain) {
		size_t n;
		if (sqfs_xattr_read(&x))
			 return EIO;
		n = sqfs_xattr_name_size(&x);
		count += n + 1;
		
		if (buf) {
			if (count > *size)
				return ERANGE;
			if (sqfs_xattr_name(&x, buf, true))
				return EIO;
			buf += n;
			*buf++ = '\0';
		}
	}
	*size = count;
	return 0;
}

void sqfs_usage(char *progname, bool fuse_usage) {
	fprintf(stderr, "%s (c) 2012 Dave Vasilevsky\n\n", PACKAGE_STRING);
	fprintf(stderr, "Usage: %s [options] ARCHIVE MOUNTPOINT\n",
		progname ? progname : PACKAGE_NAME);
	if (fuse_usage) {
		struct fuse_args args = FUSE_ARGS_INIT(0, NULL);
		fuse_opt_add_arg(&args, ""); /* progname */
		fuse_opt_add_arg(&args, "-ho");
		fprintf(stderr, "\n");
		fuse_parse_cmdline(&args, NULL, NULL, NULL);
	}
	exit(-2);
}

int sqfs_opt_proc(void *data, const char *arg, int key,
		struct fuse_args *outargs) {
	sqfs_opts *opts = (sqfs_opts*)data;
	if (key == FUSE_OPT_KEY_NONOPT) {
		if (opts->mountpoint) {
			return -1; /* Too many args */
		} else if (opts->image) {
			opts->mountpoint = 1;
			return 1;
		} else {
			opts->image = arg;
			return 0;
		}
	} else if (key == FUSE_OPT_KEY_OPT) {
		if (strncmp(arg, "-h", 2) == 0 || strncmp(arg, "--h", 3) == 0)
			sqfs_usage(opts->progname, true);
	}
	return 1; /* Keep */
}
