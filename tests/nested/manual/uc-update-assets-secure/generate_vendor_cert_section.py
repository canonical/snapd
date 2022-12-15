# Copyright (C) 2022 Canonical Ltd
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License version 3 as
# published by the Free Software Foundation.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program.  If not, see <http://www.gnu.org/licenses/>.

import struct
import argparse

# The vendor certificate table in created by cert.S in the shim project.
# See:
# https://github.com/rhboot/shim/blob/505cdb678b319fcf9a7fdee77c0f091b4147cbe5/cert.S

def main():
    parser = argparse.ArgumentParser(description='Generate a shim vendor certificate section')
    parser.add_argument('OUTPUT', help='Output file')
    parser.add_argument('CERT', help='Vendor certificate')
    parser.add_argument('DBX', nargs='?', help='Vendor dbx')
    args = parser.parse_args()
    with open(args.CERT, 'rb') as f:
        cert = f.read()
    if args.DBX is not None:
        with open(args.DBX, 'rb') as f:
            dbx = f.read()
    else:
        dbx = b''

    header_format = 'IIII'
    header_size = struct.calcsize(header_format)
    header = struct.pack(header_format, len(cert), len(dbx), header_size, header_size+len(cert))
    with open(args.OUTPUT, 'wb') as f:
        f.write(header)
        f.write(cert)
        f.write(dbx)

if __name__ == '__main__':
    main()
