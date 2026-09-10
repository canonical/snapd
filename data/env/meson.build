if snapmountdir == '/snap'
  canonical_mount_dir = 'true'
  alt_mount_dir = 'false'
else
  canonical_mount_dir = 'false'
  alt_mount_dir = 'true'
endif

custom_target(
  'snapd.sh',
  output : 'snapd.sh',
  input : 'snapd.sh.in',
  capture: true,
  install: true,
  install_dir: get_option('envd'),
  command: [
    m4, '-P', '-E',
    '-D', 'USE_CANONICAL_SNAP_MOUNT_DIR=@0@'.format(canonical_mount_dir),
    '-D', 'USE_ALT_SNAP_MOUNT_DIR=@0@'.format(alt_mount_dir),
    '@INPUT@',
  ],
)

custom_target(
  'snapd.fish',
  output: 'snapd.fish',
  input: 'snapd.fish.in',
  capture: true,
  install: true,
  install_dir: get_option('datadir') / 'fish/vendor_conf.d',
  command: [
    m4, '-P', '-E',
    '-D', 'USE_CANONICAL_SNAP_MOUNT_DIR=@0@'.format(canonical_mount_dir),
    '-D', 'USE_ALT_SNAP_MOUNT_DIR=@0@'.format(alt_mount_dir),
    '@INPUT@',
  ],
)
