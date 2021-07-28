/*
 * Copyright (C) 2021 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

#ifndef SNAP_CONFINE_BPF_SUPPORT_H
#define SNAP_CONFINE_BPF_SUPPORT_H

#include <linux/bpf.h>
#include <stddef.h>

/**
 * bpf_pin_to_path pins an object referenced by fd to a path under a bpffs
 * mount.
 */
int bpf_pin_to_path(int fd, const char *path);

/**
 * bpf_get_by_path obtains the file handle to the object referenced by a path
 * under bpffs filesystem.
 */
int bpf_get_by_path(const char *path);

/**
 * bpf_load_prog loads a given BPF program.
 *
 * Passing non-NULL log buf, will populate the buffer with output from verifier
 * if the program is found to be invalid.
 */
int bpf_load_prog(enum bpf_prog_type type, const struct bpf_insn *insns, size_t insns_cnt, char *log_buf,
                  size_t log_buf_size);

int bpf_prog_attach(enum bpf_attach_type type, int cgroup_fd, int prog_fd);

/**
 * bf_create_map creates a BPF and returns a file descriptor handle to it.
 */
int bpf_create_map(enum bpf_map_type type, size_t key_size, size_t value_size, size_t max_entries);

/**
 * bpf_update_map updates the value of element with a given key (or adds it to
 * the map).
 */
int bpf_update_map(int map_fd, const void *key, const void *value);

/**
 * bpf_map_get_next_key iterates over keys of the map.
 *
 * When key does not match anything in the map, it is set to the first element
 * of the map and next_key holds the next key. Subsequent calls will obtain the
 * next_key following key. When an end if reached, -1 is returned and error is
 * set to ENOENT.
 */
int bpf_map_get_next_key(int map_fd, const void *key, void *next_key);

/**
 * bpf_map_delete_batch performs a batch delete of elements with keys, where cnt
 * is the number of keys.
 */
int bpf_map_delete_batch(int map_fd, const void *keys, size_t cnt);

/**
 * bpf_map_delete_elem deletes an element with a key from the map, returns -1
 * and ENOENT when the element did not exist.
 */
int bpf_map_delete_elem(int map_fd, const void *key);

#endif /* SNAP_CONFINE_BPF_SUPPORT_H */
