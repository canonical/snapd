if snapmountdir == '/snap'
  canonical_mount_dir = 'true'
  alt_mount_dir = 'false'
else
  canonical_mount_dir = 'false'
  alt_mount_dir = 'true'
endif

custom_target(
  '990-snapd.conf.in',
  output: '990-snapd.conf'
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
