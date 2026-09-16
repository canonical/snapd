cfg = configuration_data({
  'STATIC_SNAP_TOOLING_DIR': get_option('prefix') / get_option('libexecdir'),
})

configure_file(
  input : 'snap-mgmt.sh.in',
  output : 'snap-mgmt',
  configuration : cfg,
  install : true,
  install_dir : get_option('libexecdir'),
  install_mode : 'rwxr-xr-x',
)

if get_option('selinux')
  cfg = configuration_data({
    'STATIC_SNAP_MOUNT_DIR': snapmountdir,
  })

  configure_file(
    input : 'snap-mgmt-selinux.sh.in',
    output : 'snap-mgmt-selinux',
    configuration : cfg,
    install : true,
    install_dir : get_option('libexecdir'),
    install_mode : 'rwxr-xr-x',
  )
endif
