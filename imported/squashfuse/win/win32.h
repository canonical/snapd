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
#ifndef SQFS_WIN32_H
#define SQFS_WIN32_H

#include <Windows.h>
#include <sys/stat.h>
#include <stdint.h>

#define S_IFIFO 0010000
#define S_IFBLK 0060000
#define S_IFLNK 0120000
#define S_IFSOCK 0140000
#define S_ISDIR(_m) (((_m) & S_IFMT) == S_IFDIR)
#define S_ISREG(_m) (((_m) & S_IFMT) == S_IFREG)
#define S_ISLNK(_m) (((_m) & S_IFMT) == S_IFLNK)
typedef unsigned short sqfs_mode_t;
typedef uint32_t sqfs_id_t; /* Internal uids/gids are 32-bits */

typedef SSIZE_T ssize_t;
typedef DWORD64 sqfs_off_t;
typedef HANDLE sqfs_fd_t;

#endif
