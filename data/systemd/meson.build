systemd_units_generated = [
  'snapd.apparmor.service',
  'snapd.failure.service',
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
  ]
endif

cfg = configuration_data({
  'bindir': get_option('prefix') / get_option('bindir'),
  'libexecdir': get_option('prefix') / get_option('libexecdir'),
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
  'snapd.gpio-chardev-setup.target',
  'snapd.mounts-pre.target',
  'snapd.mounts.target',
  'snapd.snap-repair.timer',
  'snapd.socket',
]

install_data(
  static_units,
  install_dir : systemdsystemunitdir,
)

if get_option('core')
  install_data(
    'snapd.core-fixup.sh',
    'snapd.run-from-snap',
    install_dir : get_option('prefix') / get_option('libexecdir') / 'snapd',
  )
endif
