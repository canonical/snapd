#!/usr/bin/env python3
import sys
import json
import re

class mountinfo_entry:

    def __init__(self, fs_type, mount_id, mount_opts, mount_point, mount_src, opt_fields, root_dir):
        self.fs_type = fs_type
        self.mount_id = mount_id
        self.mount_opts = mount_opts
        self.mount_point = mount_point
        self.mount_src = mount_src
        self.opt_fields = opt_fields
        self.root_dir = root_dir

    @classmethod
    def parse(cls, line):
        parts = line.split()
        fs_type = parts[-3]
        mount_id = parts[0]
        mount_opts = parts[5]
        mount_point = parts[4]
        mount_src = parts[-2]
        root_dir = parts[3]
        opt_fields = []
        i = 6
        while parts[i] != '-':
            opt = parts[i]
            opt_fields.append(opt)
            i += 1
        opt_fields.sort()
        return cls(fs_type, mount_id, mount_opts, mount_point, mount_src,
                   opt_fields, root_dir)

    def _fix_nondeterministic_mount_point(self):
        self.mount_point = re.sub('_\w{6}', '_XXXXXX', self.mount_point)
        self.mount_point = re.sub('/\d+$', '/NUMBER', self.mount_point)

    def _fix_nondeterministic_root_dir(self):
        self.root_dir = re.sub('_\w{6}', '_XXXXXX', self.root_dir)

    def _fix_nondeterministic_mount_src(self):
        self.mount_src = re.sub('/dev/[sv]da', '/dev/BLOCK', self.mount_src)

    def _fix_nondeterministic_opt_fields(self, seen):
        fixed = []
        for opt in self.opt_fields:
            if opt not in seen:
                opt_id = len(seen)
                seen[opt] = opt_id
            else:
                opt_id = seen[opt]
            remapped_opt = re.sub(':\d+$', lambda m: ':renumbered/{}'.format(opt_id), opt)
            fixed.append(remapped_opt)
        self.opt_fields = fixed

    def _fix_nondeterministic_loop(self, seen):
        if not self.mount_src.startswith("/dev/loop"):
            return
        if self.mount_src not in seen:
            loop_id = len(seen)
            seen[self.mount_src] = loop_id
        else:
            loop_id = seen[self.mount_src]
        self.mount_src = re.sub('loop\d+$', lambda m: 'remapped-loop{}'.format(loop_id), self.mount_src)

    def as_json(self):
        return {
            "fs_type": self.fs_type,
            "mount_opts": self.mount_opts,
            "mount_point": self.mount_point,
            "mount_src": self.mount_src,
            "opt_fields": self.opt_fields,
            "root_dir": self.root_dir,
        }


def parse_mountinfo(lines):
    return [mountinfo_entry.parse(line) for line in lines]


def fix_initial_nondeterminism(entries):
    for entry in entries:
        entry._fix_nondeterministic_mount_point()


def fix_remaining_nondeterminism(entries):
    seen_opt_fields = {}
    seen_loops = {}
    for entry in entries:
        entry._fix_nondeterministic_root_dir()
        entry._fix_nondeterministic_mount_src()
        entry._fix_nondeterministic_opt_fields(seen_opt_fields)
        entry._fix_nondeterministic_loop(seen_loops)


def main():
    entries = parse_mountinfo(sys.stdin)
    # Get rid of the core snap as it is not certain that we'll see one and we want determinism
    entries = [entry for entry in entries if not re.match("/snap/core/\d+", entry.mount_point)]
    # Fix random directories and non-deterministic revisions
    fix_initial_nondeterminism(entries)
    # Sort by just the mount point,
    entries.sort(key=lambda entry: (entry.mount_point))
    # Fix remainder of the non-determinism
    fix_remaining_nondeterminism(entries)
    # Make entries nicely deterministic, by sorting them by mount location
    entries.sort(key=lambda entry: (entry.mount_point, entry.mount_src, entry.root_dir))
    # Export everything
    json.dump([entry.as_json() for entry in entries],
              sys.stdout, sort_keys=True, indent=2, separators=(',', ': '))
    sys.stdout.write('\n')


if __name__ == '__main__':
    main()
