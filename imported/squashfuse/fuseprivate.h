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
#ifndef SQFS_FUSEPRIVATE_H
#define SQFS_FUSEPRIVATE_H

#include "squashfuse.h"

#include <fuse.h>

#include <sys/stat.h>

/* Common functions for FUSE high- and low-level clients */

/* Fill in a stat structure. Does not set st_ino */
sqfs_err sqfs_stat(sqfs *fs, sqfs_inode *inode, struct stat *st);

/* Populate an xattr list. Return an errno value. */
int sqfs_listxattr(sqfs *fs, sqfs_inode *inode, char *buf, size_t *size);

/* Print a usage string */
void sqfs_usage(char *progname, bool fuse_usage);

/* Parse command-line arguments */
typedef struct {
	char *progname;
	const char *image;
	int mountpoint;
	size_t offset;
} sqfs_opts;
int sqfs_opt_proc(void *data, const char *arg, int key,
	struct fuse_args *outargs);

#endif
