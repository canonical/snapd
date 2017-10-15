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
#include "swap.h"

#define SWAP(BITS) \
	void sqfs_swapin##BITS(uint##BITS##_t *v) { \
		int i; \
		uint8_t *c = (uint8_t*)v; \
		uint##BITS##_t r = 0; \
		for (i = sizeof(*v) - 1; i >= 0; --i) { \
			r <<= 8; \
			r += c[i]; \
		} \
		*v = r; \
	}

SWAP(16)
SWAP(32)
SWAP(64)
#undef SWAP

void sqfs_swapin16_internal(__le16 *v) { sqfs_swapin16((uint16_t*)v); }
void sqfs_swapin32_internal(__le32 *v) { sqfs_swapin32((uint32_t*)v); }
void sqfs_swapin64_internal(__le64 *v) { sqfs_swapin64((uint64_t*)v); }

void sqfs_swap16(uint16_t *n) {
	*n = (*n >> 8) + (*n << 8);
}

#include "squashfs_fs.h"
#include "swap.c.inc"
