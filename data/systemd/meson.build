systemd_units_generated = [
  # FIXME: is this really going to work when we do not have libapparmor for snap-confine?
  'snapd.apparmor.service',
  'snapd.seeded.service',
  'snapd.service',
]

if get_option('core')
  systemd_units_generated += [
    'snapd.core-fixup.service',
    'snapd.autoimport.service',
    'snapd.snap-repair.service',
    'snapd.system-shutdown.service',
    'snapd.recovery-chooser-trigger.service',
    'snapd.failure.service',
  ]
endif

cfg = configuration_data({
  'bindir': get_option('prefix') / get_option('bindir'),
  # FIXME: ./cmd autotools expect --libexecdir with snapd included, and Makefile in ./data do not.
  'libexecdir': get_option('prefix') / get_option('libexecdir') / '..',
  'SNAPD_ENVIRONMENT_FILE': get_option('snapd_environment_file'),
})

foreach generated_unit : systemd_units_generated
  configure_file(
    input : generated_unit + '.in',
    output : generated_unit,
    configuration : cfg,
    install : true,
    install_dir : systemdsystemunitdir,
  )
endforeach

static_units = [
  'snapd.mounts-pre.target',
  'snapd.mounts.target',
  'snapd.socket',
]

install_data(
  static_units,
  install_dir : systemdsystemunitdir,
)

install_data(
  'snapd.run-from-snap',
  install_dir : get_option('libexecdir'),
)

if get_option('core')
  install_data(
    'snapd.gpio-chardev-setup.target',
    'snapd.snap-repair.timer',
    install_dir : systemdsystemunitdir,
  )

  install_data(
    'snapd.core-fixup.sh',
    install_dir : get_option('libexecdir'),
  )
endif
