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
#include "ll.h"
#include "fuseprivate.h"

#include "nonstd.h"

#include <errno.h>
#include <float.h>
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const double SQFS_TIMEOUT = DBL_MAX;

static void sqfs_ll_op_getattr(fuse_req_t req, fuse_ino_t ino,
		struct fuse_file_info *fi) {
	sqfs_ll_i lli;
	struct stat st;
	if (sqfs_ll_iget(req, &lli, ino))
		return;
	
	if (sqfs_stat(&lli.ll->fs, &lli.inode, &st)) {
		fuse_reply_err(req, ENOENT);
	} else {
		st.st_ino = ino;
		fuse_reply_attr(req, &st, SQFS_TIMEOUT);
	}
}

static void sqfs_ll_op_opendir(fuse_req_t req, fuse_ino_t ino,
		struct fuse_file_info *fi) {
	sqfs_ll_i *lli;
	
	fi->fh = (intptr_t)NULL;
	
	lli = malloc(sizeof(*lli));
	if (!lli) {
		fuse_reply_err(req, ENOMEM);
		return;
	}
	
	if (sqfs_ll_iget(req, lli, ino) == SQFS_OK) {
		if (!S_ISDIR(lli->inode.base.mode)) {
			fuse_reply_err(req, ENOTDIR);
		} else {
			fi->fh = (intptr_t)lli;
			fuse_reply_open(req, fi);
			return;
		}
	}
	free(lli);
}

static void sqfs_ll_op_create(fuse_req_t req, fuse_ino_t parent, const char *name,
			      mode_t mode, struct fuse_file_info *fi) {
	fuse_reply_err(req, EROFS);
}

static void sqfs_ll_op_releasedir(fuse_req_t req, fuse_ino_t ino,
		struct fuse_file_info *fi) {
	free((sqfs_ll_i*)(intptr_t)fi->fh);
	fuse_reply_err(req, 0); /* yes, this is necessary */
}

static size_t sqfs_ll_add_direntry(fuse_req_t req, char *buf, size_t bufsize,
		const char *name, const struct stat *st, off_t off) {
	#if HAVE_DECL_FUSE_ADD_DIRENTRY
		return fuse_add_direntry(req, buf, bufsize, name, st, off);
	#else
		size_t esize = fuse_dirent_size(strlen(name));
		if (bufsize >= esize)
			fuse_add_dirent(buf, name, st, off);
		return esize;
	#endif
}
static void sqfs_ll_op_readdir(fuse_req_t req, fuse_ino_t ino, size_t size,
		off_t off, struct fuse_file_info *fi) {
	sqfs_err sqerr;
	sqfs_dir dir;
	sqfs_name namebuf;
	sqfs_dir_entry entry;
	size_t esize;
	struct stat st;
	
	char *buf = NULL, *bufpos = NULL;
	sqfs_ll_i *lli = (sqfs_ll_i*)(intptr_t)fi->fh;
	int err = 0;
	
	if (sqfs_dir_open(&lli->ll->fs, &lli->inode, &dir, off))
		err = EINVAL;
	if (!err && !(bufpos = buf = malloc(size)))
		err = ENOMEM;
	
	if (!err) {
		memset(&st, 0, sizeof(st));
		sqfs_dentry_init(&entry, namebuf);
		while (sqfs_dir_next(&lli->ll->fs, &dir, &entry, &sqerr)) {
			st.st_ino = lli->ll->ino_fuse_num(lli->ll, &entry);
			st.st_mode = sqfs_dentry_mode(&entry);
		
			esize = sqfs_ll_add_direntry(req, bufpos, size, sqfs_dentry_name(&entry),
				&st, sqfs_dentry_next_offset(&entry));
			if (esize > size)
				break;
		
			bufpos += esize;
			size -= esize;
		}
		if (sqerr)
			err = EIO;
	}
	
	if (err)
		fuse_reply_err(req, err);
	else
		fuse_reply_buf(req, buf, bufpos - buf);
	free(buf);
}

static void sqfs_ll_op_lookup(fuse_req_t req, fuse_ino_t parent,
		const char *name) {
	sqfs_ll_i lli;
	sqfs_err sqerr;
	sqfs_name namebuf;
	sqfs_dir_entry entry;
	bool found;
	sqfs_inode inode;
	
	if (sqfs_ll_iget(req, &lli, parent))
		return;
	
	if (!S_ISDIR(lli.inode.base.mode)) {
		fuse_reply_err(req, ENOTDIR);
		return;
	}
	
	sqfs_dentry_init(&entry, namebuf);
	sqerr = sqfs_dir_lookup(&lli.ll->fs, &lli.inode, name, strlen(name), &entry,
		&found);
	if (sqerr) {
		fuse_reply_err(req, EIO);
		return;
	}
	if (!found) {
		fuse_reply_err(req, ENOENT);
		return;
	}
	
	if (sqfs_inode_get(&lli.ll->fs, &inode, sqfs_dentry_inode(&entry))) {
		fuse_reply_err(req, ENOENT);
	} else {
		struct fuse_entry_param fentry;
		memset(&fentry, 0, sizeof(fentry));
		if (sqfs_stat(&lli.ll->fs, &inode, &fentry.attr)) {
			fuse_reply_err(req, EIO);
		} else {
			fentry.attr_timeout = fentry.entry_timeout = SQFS_TIMEOUT;
			fentry.ino = lli.ll->ino_register(lli.ll, &entry);
			fentry.attr.st_ino = fentry.ino;
			fuse_reply_entry(req, &fentry);
		}
	}
}

static void sqfs_ll_op_open(fuse_req_t req, fuse_ino_t ino,
		struct fuse_file_info *fi) {
	sqfs_inode *inode;
	sqfs_ll *ll;
	
	if (fi->flags & (O_WRONLY | O_RDWR)) {
		fuse_reply_err(req, EROFS);
		return;
	}
	
	inode = malloc(sizeof(sqfs_inode));
	if (!inode) {
		fuse_reply_err(req, ENOMEM);
		return;
	}
	
	ll = fuse_req_userdata(req);
	if (sqfs_ll_inode(ll, inode, ino)) {
		fuse_reply_err(req, ENOENT);
	} else if (!S_ISREG(inode->base.mode)) {
		fuse_reply_err(req, EISDIR);
	} else {
		fi->fh = (intptr_t)inode;
		fi->keep_cache = 1;
		fuse_reply_open(req, fi);
		return;
	}
	free(inode);
}

static void sqfs_ll_op_release(fuse_req_t req, fuse_ino_t ino,
		struct fuse_file_info *fi) {
	free((sqfs_inode*)(intptr_t)fi->fh);
	fi->fh = 0;
	fuse_reply_err(req, 0);
}

static void sqfs_ll_op_read(fuse_req_t req, fuse_ino_t ino,
		size_t size, off_t off, struct fuse_file_info *fi) {
	sqfs_ll *ll = fuse_req_userdata(req);
	sqfs_inode *inode = (sqfs_inode*)(intptr_t)fi->fh;
	sqfs_err err = SQFS_OK;
	
	off_t osize;
	char *buf = malloc(size);
	if (!buf) {
		fuse_reply_err(req, ENOMEM);
		return;
	}
	
	osize = size;
	err = sqfs_read_range(&ll->fs, inode, off, &osize, buf);
	if (err) {
		fuse_reply_err(req, EIO);
	} else if (osize == 0) { /* EOF */
		fuse_reply_buf(req, NULL, 0);
	} else {
		fuse_reply_buf(req, buf, osize);
	}
	free(buf);
}

static void sqfs_ll_op_readlink(fuse_req_t req, fuse_ino_t ino) {
	char *dst;
	size_t size;
	sqfs_ll_i lli;
	if (sqfs_ll_iget(req, &lli, ino))
		return;
	
	if (!S_ISLNK(lli.inode.base.mode)) {
		fuse_reply_err(req, EINVAL);
	} else if (sqfs_readlink(&lli.ll->fs, &lli.inode, NULL, &size)) {
		fuse_reply_err(req, EIO);
	} else if (!(dst = malloc(size + 1))) {
		fuse_reply_err(req, ENOMEM);
	} else if (sqfs_readlink(&lli.ll->fs, &lli.inode, dst, &size)) {
		fuse_reply_err(req, EIO);
		free(dst);
	} else {
		fuse_reply_readlink(req, dst);
		free(dst);
	}
}

static void sqfs_ll_op_listxattr(fuse_req_t req, fuse_ino_t ino, size_t size) {
	sqfs_ll_i lli;
	char *buf;
	int ferr;
	
	if (sqfs_ll_iget(req, &lli, ino))
		return;

	buf = NULL;
	if (size && !(buf = malloc(size))) {
		fuse_reply_err(req, ENOMEM);
		return;
	}
	
	ferr = sqfs_listxattr(&lli.ll->fs, &lli.inode, buf, &size);
	if (ferr) {
		fuse_reply_err(req, ferr);
	} else if (buf) {
		fuse_reply_buf(req, buf, size);
	} else {
		fuse_reply_xattr(req, size);
	}
	free(buf);
}

static void sqfs_ll_op_getxattr(fuse_req_t req, fuse_ino_t ino,
		const char *name, size_t size
#ifdef FUSE_XATTR_POSITION
		, uint32_t position
#endif
		) {
	sqfs_ll_i lli;
	char *buf = NULL;
	size_t real = size;

#ifdef FUSE_XATTR_POSITION
	if (position != 0) { /* We don't support resource forks */
		fuse_reply_err(req, EINVAL);
		return;
	}
#endif
	
	if (sqfs_ll_iget(req, &lli, ino))
		return;
	
	if (!(buf = malloc(size)))
		fuse_reply_err(req, ENOMEM);
	else if (sqfs_xattr_lookup(&lli.ll->fs, &lli.inode, name, buf, &real))
		fuse_reply_err(req, EIO);
	else if (real == 0)
		fuse_reply_err(req, sqfs_enoattr());
	else if (size == 0)
		fuse_reply_xattr(req, real);
	else if (size < real)
		fuse_reply_err(req, ERANGE);
	else
		fuse_reply_buf(req, buf, real);
	free(buf);
}

static void sqfs_ll_op_forget(fuse_req_t req, fuse_ino_t ino,
		unsigned long nlookup) {
	sqfs_ll_i lli;
	sqfs_ll_iget(req, &lli, SQFS_FUSE_INODE_NONE);
	lli.ll->ino_forget(lli.ll, ino, nlookup);
	fuse_reply_none(req);
}


/* Helpers to abstract out FUSE 2.5 vs 2.6+ differences */

typedef struct {
	int fd;
	struct fuse_chan *ch;
} sqfs_ll_chan;

static sqfs_err sqfs_ll_mount(sqfs_ll_chan *ch, const char *mountpoint,
		struct fuse_args *args) {
	#ifdef HAVE_NEW_FUSE_UNMOUNT
		ch->ch = fuse_mount(mountpoint, args);
	#else
		ch->fd = fuse_mount(mountpoint, args);
		if (ch->fd == -1)
			return SQFS_ERR;
		ch->ch = fuse_kern_chan_new(ch->fd);
	#endif
	return ch->ch ? SQFS_OK : SQFS_ERR;
}

static void sqfs_ll_unmount(sqfs_ll_chan *ch, const char *mountpoint) {
	#ifdef HAVE_NEW_FUSE_UNMOUNT
		fuse_unmount(mountpoint, ch->ch);
	#else
		close(ch->fd);
		fuse_unmount(mountpoint);
	#endif
}

static sqfs_ll *sqfs_ll_open(const char *path, size_t offset) {
	sqfs_ll *ll;
	
	ll = malloc(sizeof(*ll));
	if (!ll) {
		perror("Can't allocate memory");
	} else {
		memset(ll, 0, sizeof(*ll));
		ll->fs.offset = offset;
		if (sqfs_open_image(&ll->fs, path, offset) == SQFS_OK) {
			if (sqfs_ll_init(ll))
				fprintf(stderr, "Can't initialize this filesystem!\n");
			else
				return ll;
			sqfs_destroy(&ll->fs);
		}
		
		free(ll);
	}
	return NULL;
}

int main(int argc, char *argv[]) {
	struct fuse_args args;
	sqfs_opts opts;
	
	char *mountpoint = NULL;
	int mt, fg;
	
	int err;
	sqfs_ll *ll;
	struct fuse_opt fuse_opts[] = {
		{"offset=%u", offsetof(sqfs_opts, offset), 0},
		FUSE_OPT_END
	};
	
	struct fuse_lowlevel_ops sqfs_ll_ops;
	memset(&sqfs_ll_ops, 0, sizeof(sqfs_ll_ops));
	sqfs_ll_ops.getattr		= sqfs_ll_op_getattr;
	sqfs_ll_ops.opendir		= sqfs_ll_op_opendir;
	sqfs_ll_ops.releasedir	= sqfs_ll_op_releasedir;
	sqfs_ll_ops.readdir		= sqfs_ll_op_readdir;
	sqfs_ll_ops.lookup		= sqfs_ll_op_lookup;
	sqfs_ll_ops.open		= sqfs_ll_op_open;
	sqfs_ll_ops.create		= sqfs_ll_op_create;
	sqfs_ll_ops.release		= sqfs_ll_op_release;
	sqfs_ll_ops.read		= sqfs_ll_op_read;
	sqfs_ll_ops.readlink	= sqfs_ll_op_readlink;
	sqfs_ll_ops.listxattr	= sqfs_ll_op_listxattr;
	sqfs_ll_ops.getxattr	= sqfs_ll_op_getxattr;
	sqfs_ll_ops.forget		= sqfs_ll_op_forget;
   
	/* PARSE ARGS */
	args.argc = argc;
	args.argv = argv;
	args.allocated = 0;
	
	opts.progname = argv[0];
	opts.image = NULL;
	opts.mountpoint = 0;
	opts.offset = 0;
	if (fuse_opt_parse(&args, &opts, fuse_opts, sqfs_opt_proc) == -1)
		sqfs_usage(argv[0], true);

	if (fuse_parse_cmdline(&args, &mountpoint, &mt, &fg) == -1)
		sqfs_usage(argv[0], true);
	if (mountpoint == NULL)
		sqfs_usage(argv[0], true);
	
	/* OPEN FS */
	err = !(ll = sqfs_ll_open(opts.image, opts.offset));
	
	/* STARTUP FUSE */
	if (!err) {
		sqfs_ll_chan ch;
		err = -1;
		if (sqfs_ll_mount(&ch, mountpoint, &args) == SQFS_OK) {
			struct fuse_session *se = fuse_lowlevel_new(&args,
				&sqfs_ll_ops, sizeof(sqfs_ll_ops), ll);	
			if (se != NULL) {
				if (sqfs_ll_daemonize(fg) != -1) {
					if (fuse_set_signal_handlers(se) != -1) {
						fuse_session_add_chan(se, ch.ch);
						/* FIXME: multithreading */
						err = fuse_session_loop(se);
						fuse_remove_signal_handlers(se);
						#if HAVE_DECL_FUSE_SESSION_REMOVE_CHAN
							fuse_session_remove_chan(ch.ch);
						#endif
					}
				}
				fuse_session_destroy(se);
			}
			sqfs_ll_destroy(ll);
			sqfs_ll_unmount(&ch, mountpoint);
		}
	}
	fuse_opt_free_args(&args);
	free(ll);
	free(mountpoint);
	
	return -err;
}
