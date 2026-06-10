import socket
import struct

NETLINK_AUDIT = 9
AUDIT_SET = 1001
NLM_F_REQUEST = 1

# audit_status struct: mask=1 (AUDIT_STATUS_ENABLED), enabled=1, rest zeroed
status = struct.pack('IIIIIIiii', 1, 1, 0, 0, 0, 0, 0, 0, 0)
hdr = struct.pack('IHHII', 16 + len(status), AUDIT_SET, NLM_F_REQUEST, 1, 0)
sock = socket.socket(socket.AF_NETLINK, socket.SOCK_RAW, NETLINK_AUDIT)
sock.sendto(hdr + status, (0, 0))
sock.close()
