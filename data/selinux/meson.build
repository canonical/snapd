if get_option('selinux').enabled() or (get_option('selinux').auto() and libselinux.found())
  install_data(
    'snappy.if',
    install_dir : get_option('datadir') / 'selinux/devel/include/contrib'
  )

  snappy_pp_source = custom_target(
    'snappy-pp-source',
    output : [
      'snappy.te',
      'snappy.fc',
      'snappy.if',
    ],
    input : [
      'snappy.te',
      'snappy.fc',
      'snappy.if',
    ],
    command : [
      'cp', '-t', '@OUTDIR@', '@INPUT@',
    ],
  )
  snappy_pp = custom_target(
    'snappy.pp',
    output : 'snappy.pp',
    depends : [snappy_pp_source],
    command : [
      'make', '-C', '@OUTDIR@', '-f', '/usr/share/selinux/devel/Makefile', 'snappy.pp',
    ],
  )

  bzip2 = find_program('bzip2')

  custom_target(
    'snappy.pp.bz2',
    output : 'snappy.pp.bz2',
    input : snappy_pp,
    capture : true,
    command : [bzip2, '-9', '-ck', '@INPUT@'],
    install : true,
    install_dir : get_option('datadir') / 'selinux/packages',
  )
endif