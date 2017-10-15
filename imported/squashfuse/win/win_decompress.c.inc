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
#include "decompress.h"

#include "squashfuse.h"

#include <string.h>

enum
{
	TINFL_FLAG_PARSE_ZLIB_HEADER = 1,
	TINFL_FLAG_HAS_MORE_INPUT = 2,
	TINFL_FLAG_USING_NON_WRAPPING_OUTPUT_BUF = 4,
	TINFL_FLAG_COMPUTE_ADLER32 = 8
};
#define TINFL_DECOMPRESS_MEM_TO_MEM_FAILED ((size_t)(-1))
size_t tinfl_decompress_mem_to_mem(void *pOut_buf, size_t out_buf_len,
	const void *pSrc_buf, size_t src_buf_len, int flags);

static sqfs_err sqfs_decompressor_zlib(void *in, size_t insz,
		void *out, size_t *outsz) {
	size_t bytes = tinfl_decompress_mem_to_mem(out, *outsz, in, insz,
		TINFL_FLAG_PARSE_ZLIB_HEADER | TINFL_FLAG_USING_NON_WRAPPING_OUTPUT_BUF);
	if (bytes == TINFL_DECOMPRESS_MEM_TO_MEM_FAILED)
		return SQFS_ERR;
	*outsz = bytes;
	return SQFS_OK;
}
#define CAN_DECOMPRESS_ZLIB 1
