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
#include "squashfuse.h"

#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>


#define PROGNAME "squashfuse_ls"

#define ERR_MISC	(-1)
#define ERR_USAGE	(-2)
#define ERR_OPEN	(-3)

static void usage() {
	fprintf(stderr, "%s (c) 2013 Dave Vasilevsky\n\n", PROGNAME);
	fprintf(stderr, "Usage: %s ARCHIVE\n", PROGNAME);
	exit(ERR_USAGE);
}

static void die(const char *msg) {
	fprintf(stderr, "%s\n", msg);
	exit(ERR_MISC);
}

int main(int argc, char *argv[]) {
	sqfs_err err = SQFS_OK;
	sqfs_traverse trv;
	sqfs fs;
	char *image;

	if (argc != 2)
		usage();
	image = argv[1];

	if ((err = sqfs_open_image(&fs, image, 0)))
		exit(ERR_OPEN);
	
	if ((err = sqfs_traverse_open(&trv, &fs, sqfs_inode_root(&fs))))
		die("sqfs_traverse_open error");
	while (sqfs_traverse_next(&trv, &err)) {
		if (!trv.dir_end) {
			printf("%s\n", trv.path);
		}
	}
	if (err)
		die("sqfs_traverse_next error");
	sqfs_traverse_close(&trv);
	
	sqfs_fd_close(fs.fd);
	return 0;
}
