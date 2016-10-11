#!/usr/bin/env python3
import sys
import json
import re

_boring_fs = set([])

def line2mountinfo(lines, boring_fs=_boring_fs):
    seen = {}
    for line in lines:
        parts = line.split()
        fs_type = parts[-3]
        if fs_type in boring_fs:
            continue
        mount_src = parts[-2]
        root_dir = re.sub('_\w{6}', '_XXXXXX', parts[3])
        mount_point = re.sub('/\d+$', '/NUMBER', parts[4])
        mount_point = re.sub('_\w{6}', '_XXXXXX', mount_point)
        if mount_point == "/snap/core/NUMBER":
            # Skip the core snap for now, ideally this would be better handled
            # but depending on test ordering there are two possible outcomes.
            continue
        mount_opts = parts[5]
        opt_fields = []
        i = 6
        while parts[i] != '-':
            opt = parts[i]
            # renumber options
            if opt not in seen:
                opt_id = len(seen)
                seen[opt] = opt_id
            else:
                opt_id = seen[opt]
            opt_fields.append(re.sub(':\d+$', lambda m: ':renumbered/{}'.format(opt_id), parts[i]))
            i += 1
        yield {
            'root_dir': root_dir,
            'mount_point': mount_point,
            'mount_opts': mount_opts,
            'opt_fields': opt_fields,
            'fs_type': fs_type,
        }


def main():
    entries = list(line2mountinfo(sorted(sys.stdin)))
    entries.sort(key=lambda obj: tuple(sorted(obj.items())))
    json.dump(entries, sys.stdout, sort_keys=True,
              indent=2, separators=(',', ': '))
    sys.stdout.write('\n')


if __name__ == '__main__':
    main()
