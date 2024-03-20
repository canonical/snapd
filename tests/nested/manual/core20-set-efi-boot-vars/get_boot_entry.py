import argparse
import os
import struct

parser = argparse.ArgumentParser()
parser.add_argument("--current", action='store_true')
parser.add_argument("--path", action='store_true')
parser.add_argument("vardir")
args = parser.parse_args()

u32_f = '@I'
u16_f = '@H'
device_path_header_f = '@BBH'

def file_read(fd, fmt):
    buf = fd.read(struct.calcsize(fmt))
    if len(buf) == 0:
        return None
    return struct.unpack(fmt, buf)

order = []
with open(os.path.join(args.vardir, 'BootOrder-8be4df61-93ca-11d2-aa0d-00e098032b8c'), 'br') as b:
    file_read(b, u32_f)
    while True:
        r = file_read(b, u16_f)
        if r is None:
            break
        order.append(r[0])

with open(os.path.join(args.vardir, 'BootCurrent-8be4df61-93ca-11d2-aa0d-00e098032b8c'), 'br') as b:
    file_read(b, u32_f)
    r = file_read(b, u16_f)
    current = r[0]

if args.current:
    selected = current
else:
    selected = order[0]

with open(os.path.join(args.vardir, 'Boot{:04x}-8be4df61-93ca-11d2-aa0d-00e098032b8c'.format(selected)), 'br') as e:
    file_read(e, u32_f)
    file_read(e, u32_f)
    path_len = file_read(e, u16_f)[0]
    title_words = []
    while True:
        b = file_read(e, u16_f)
        if b[0] == 0:
            break
        title_words.append(struct.pack(u16_f, b[0]))
    last_path = None
    path_read = 0
    while path_read < path_len:
        dp_header = file_read(e, device_path_header_f)
        dp_data = e.read(dp_header[2] - struct.calcsize(device_path_header_f))
        path_read += dp_header[2]
        if dp_header[0] == 4 and dp_header[1] == 4:
            last_path = dp_data

if args.path:
    print(last_path.decode('utf-16').rstrip('\0'))
else:
    print(b''.join(title_words).decode('utf-16'))
