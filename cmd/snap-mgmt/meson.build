cfg = configuration_data({
  'STATIC_SNAP_TOOLING_DIR': get_option('prefix') / snapd_libexecdir,
})

configure_file(
  input : 'snap-mgmt.sh.in',
  output : 'snap-mgmt',
  configuration : cfg,
  install : true,
  install_dir : snapd_libexecdir,
  install_mode : 'rwxr-xr-x',
)

if libselinux.found()
  cfg = configuration_data({
    'STATIC_SNAP_MOUNT_DIR': snapmountdir,
  })

  configure_file(
    input : 'snap-mgmt-selinux.sh.in',
    output : 'snap-mgmt-selinux',
    configuration : cfg,
    install : true,
    install_dir : snapd_libexecdir,
    install_mode : 'rwxr-xr-x',
  )
endif
