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
#include "table.h"

#include "fs.h"
#include "nonstd.h"
#include "squashfs_fs.h"
#include "swap.h"

#include <stdlib.h>
#include <string.h>

sqfs_err sqfs_table_init(sqfs_table *table, sqfs_fd_t fd, sqfs_off_t start, size_t each,
		size_t count) {
	size_t i;
	size_t nblocks, bread;
	
	if (count == 0)
		return SQFS_OK;
	
	nblocks = sqfs_divceil(each * count, SQUASHFS_METADATA_SIZE);
	bread = nblocks * sizeof(uint64_t);
	
	table->each = each;
	if (!(table->blocks = malloc(bread)))
		goto err;
	if (sqfs_pread(fd, table->blocks, bread, start) != bread)
		goto err;
	
	for (i = 0; i < nblocks; ++i)
		sqfs_swapin64(&table->blocks[i]);
	
	return SQFS_OK;
	
err:
	free(table->blocks);
	table->blocks = NULL;
	return SQFS_ERR;
}

void sqfs_table_destroy(sqfs_table *table) {
	free(table->blocks);
	table->blocks = NULL;
}

sqfs_err sqfs_table_get(sqfs_table *table, sqfs *fs, size_t idx, void *buf) {
	sqfs_block *block;
	size_t pos = idx * table->each;
	size_t bnum = pos / SQUASHFS_METADATA_SIZE,
		off = pos % SQUASHFS_METADATA_SIZE;
	
	sqfs_off_t bpos = table->blocks[bnum];
	if (sqfs_md_cache(fs, &bpos, &block))
		return SQFS_ERR;
	
	memcpy(buf, (char*)(block->data) + off, table->each);
	/* BLOCK CACHED, DON'T DISPOSE */
	return SQFS_OK;
}
