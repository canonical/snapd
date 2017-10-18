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
#ifndef SQFS_XATTR_H
#define SQFS_XATTR_H

#include "common.h"

#include "squashfs_fs.h"


/* Initialize xattr handling for this fs */
sqfs_err sqfs_xattr_init(sqfs *fs);


/* xattr iterator */
typedef struct {
	sqfs *fs;	
	int cursors;
	sqfs_md_cursor c_name, c_vsize, c_val, c_next;
	
	size_t remain;
	struct squashfs_xattr_id info;
	
	uint16_t type;
	bool ool;
	struct squashfs_xattr_entry entry;
	struct squashfs_xattr_val val;
} sqfs_xattr;

/* Get xattr iterator for this inode */
sqfs_err sqfs_xattr_open(sqfs *fs, sqfs_inode *inode, sqfs_xattr *x);

/* Get new xattr entry. Call while x->remain > 0 */
sqfs_err sqfs_xattr_read(sqfs_xattr *x);

/* Accessors on xattr entry. No null-termination! */
size_t sqfs_xattr_name_size(sqfs_xattr *x);
sqfs_err sqfs_xattr_name(sqfs_xattr *x, char *name, bool prefix);
sqfs_err sqfs_xattr_value_size(sqfs_xattr *x, size_t *size);
/* Yield first 'size' bytes */
sqfs_err sqfs_xattr_value(sqfs_xattr *x, void *buf);

/* Find an xattr entry */
sqfs_err sqfs_xattr_find(sqfs_xattr *x, const char *name, bool *found);

/* Helper to find an xattr value on an inode.
   Returns in 'size' the size of the xattr, if found, or zero if not found.
   Does not touch 'buf' if it's not big enough. */
sqfs_err sqfs_xattr_lookup(sqfs *fs, sqfs_inode *inode, const char *name,
	void *buf, size_t *size);

#endif
