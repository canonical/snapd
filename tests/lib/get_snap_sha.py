#!/usr/bin/env python3

from urllib import request
from pprint import pprint
import json
import sys


if len(sys.argv) != 4:
	print("Number of parameters extected is 2: snap, channel and architecture")
	sys.exit(1)

snap = sys.argv[1]
channel = sys.argv[2]
arch = sys.argv[3]

info_request='https://api.snapcraft.io/v2/snaps/info/{}'.format(snap)
snap_info=json.loads(request.urlopen(request.Request(info_request, headers={'Snap-Device-Series': '16'})).read().decode('utf-8'))

for channel_info in snap_info.get('channel-map'):
	if channel_info.get('channel').get('risk') == channel and channel_info.get('channel').get('architecture') == arch:
		print(channel_info.get('download').get('sha3-384'))
