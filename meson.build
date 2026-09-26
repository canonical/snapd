project(
  'snapd',
  'c',
  meson_version : '>=0.61.0',
  version : run_command('version.sh', 'get', check : true).stdout().strip(),
)

meson.add_dist_script('version.sh', 'set', meson.project_version())
if get_option('vendor-dist')
  meson.add_dist_script('vendor.sh')
endif

fs = import('fs')

if get_option('vendored').auto() and fs.exists('vendor/modules.txt')
  go_mod_args = ['-mod=vendor']
elif get_option('vendored').enabled()
  go_mod_args = ['-mod=vendor']
else
  go_mod_args = []
endif

libcap = dependency('libcap', required : true)
libudev = dependency('libudev', required : true)
libseccomp = dependency('libseccomp', required : true)
libselinux = dependency('libselinux', required : get_option('libselinux'))
libapparmor = dependency('libapparmor', required : get_option('libapparmor'))

systemd = dependency('systemd', required : true)
systemdsystemgeneratordir = systemd.get_variable('systemdsystemgeneratordir')
systemdsystemenvironmentgenertorsdir = systemd.get_variable('systemdutildir') / 'system-environment-generators'
systemdsystemunitdir = systemd.get_variable('systemdsystemunitdir')
systemduserunitdir = systemd.get_variable('systemduserunitdir')
tmpfilesdir = systemd.get_variable('tmpfilesdir')

dbus = dependency('dbus-1', required : true)
session_bus_services_dir = dbus.get_variable('session_bus_services_dir')
dbusdatadir = dbus.get_variable('datadir') / 'dbus-1'

snapmountdir = get_option('snapmountdir')
snapmountdir_escaped = run_command(
  'systemd-escape', '-p', snapmountdir,
  capture : true,
  check : true,
)

dracut = dependency('dracut', required : get_option('dracut'))

glib = dependency('glib-2.0', required : get_option('unit-tests'))

meson.get_compiler('c').check_header('xfs/xqm.h', required : true)

bpf = meson.get_compiler('c').check_header('linux/bpf.h', required : get_option('bpf'))

if get_option('downstream_version_suffix') != ''
  build_version = '@0@@1@'.format(meson.project_version(), get_option('downstream_version_suffix'))
else
  build_version = meson.project_version()
endif

conf_data = configuration_data()
conf_data.set_quoted('PACKAGE_VERSION', build_version)
conf_data.set_quoted('PACKAGE', meson.project_name())
conf_data.set_quoted('STATIC_SNAP_MOUNT_DIR', snapmountdir)
conf_data.set_quoted('SNAP_MOUNT_DIR_SYSTEMD_UNIT', snapmountdir_escaped.stdout().strip())
conf_data.set('ENABLE_BPF', bpf)
conf_data.set('NVIDIA_BIARCH', get_option('nvidia-biarch'))
conf_data.set('NVIDIA_MULTIARCH', get_option('nvidia-multiarch'))
if get_option('nvidia-biarch')
  conf_data.set_quoted('LIB32_DIR', get_option('32bit-libdir'))
endif
if get_option('nvidia-multiarch')
  conf_data.set_quoted('HOST_ARCH_TRIPLET', get_option('host-arch-triplet'))
  conf_data.set_quoted('HOST_ARCH32_TRIPLET', get_option('host-arch-32bit-triplet'))
endif
conf_data.set('HAVE_SELINUX', libselinux.found())
conf_data.set('HAVE_APPARMOR', libapparmor.found())
configure_file(input : 'config.h.in',
               output : 'config.h',
               configuration : conf_data)
configuration_inc = include_directories('.')

add_project_arguments('-DHAVE_CONFIG_H', language : 'c')

goenv = environment()

gopath = get_option('gopath')
if gopath != ''
  goenv.set('GOPATH', gopath)
endif

rst2man = find_program('rst2man', required : false)

go = find_program('go', required : true)
# FIXME: go 1.20+ supports -C directly, then we should drop "env -C"
env = find_program('env', required : true)

global_tags = []

user_gotags = get_option('gotags')
if user_gotags != ''
  global_tags += user_gotags.split()
endif

if not get_option('secboot')
  global_tags += ['nosecboot']
endif
if get_option('test-keys')
  global_tags += [
    'withtestkeys',
    'withbootassetstesting',
    'structuredlogging',
    'faultinject',
    'statelocktrace',
  ]
endif
if get_option('usergo')
  global_tags += ['snapdusergo', 'osusergo']
endif

optee_tags = []
if get_option('optee')
   optee_tags += ['optee']
endif

# name, tags, static
go_binaries = [
  ['snapd', [] + optee_tags, false],
  ['snapctl', ['osusergo', 'snapdusergo'], true],
  ['snap-seccomp', [], false],
  ['snap-update-ns', ['osusergo', 'snapdusergo'], true],
]

if get_option('core')
  go_binaries += [
    ['snap-bootstrap', ['nomanagers'] + optee_tags, false],
    ['snap-repair', [], false],
    ['snap-failure', [], false],
  ]
endif

go_binary_targets = {}

global_extldflags = []
global_ldflags = []

goldflags = get_option('goldflags')
if goldflags != ''
  global_extldflags += [goldflags]
endif

if get_option('downstream_version_suffix') != ''
  global_ldflags += ['-X github.com/snapcore/snapd/snapdtool.DownstreamVersionSuffix=@0@'.format(get_option('downstream_version_suffix'))]
endif

foreach b : go_binaries
  go_binary = b[0]
  bin_tags = b[1]
  is_static = b[2]
  extldflags = global_extldflags
  if is_static
    if get_option('b_pie')
      extldflags+=['-static-pie']
      buildmode='pie'
    else
      extldflags+=['-static']
      buildmode='default'
    endif
  else
    buildmode='pie'
  endif
  ldflags = global_ldflags
  if extldflags.length() != 0
    ldflags += ['-linkmode external -extldflags "@0@"'.format(' '.join(extldflags))]
  endif
  if ldflags.length() != 0
    ldflags_processed = ['-ldflags', ' '.join(ldflags)]
  else
    ldflags_processed = []
  endif

  all_tags = global_tags + bin_tags
  if all_tags.length() == 0
    tag_args = []
  else
    tag_args = ['-tags', ' '.join(all_tags)]
    all_tags += []
  endif
  tgt = custom_target(
    go_binary,
    build_by_default: true,
    build_always_stale: true,
    output: [
      go_binary,
    ],
    install: true,
    install_dir: [
      get_option('libexecdir'),
    ],
    env : goenv,
    command: [
      env, '-C', meson.current_source_dir(), go.full_path(), 'build', '-buildmode=@0@'.format(buildmode),
    ] +
    ldflags_processed +
    go_mod_args + tag_args + [
      '-o', meson.current_build_dir() / go_binary, './cmd' / go_binary,
    ]
  )
  go_binary_targets += {go_binary: tgt}
endforeach

custom_target(
  'man.8',
  output : 'snap.8',
  command : [meson.current_source_dir() / 'cmd/snapd/snap-cli.sh', go_binary_targets['snapd'].full_path(), 'help', '--man'],
  capture : true,
  depends : go_binary_targets['snapd'],
  install : true,
  install_dir : get_option('mandir') / 'man8',
)

if global_tags.length() == 0
  global_tag_args = []
else
  global_tag_args = ['-tags', ' '.join(global_tags)]
endif

test('go', env,
  args : [
    '-C', meson.current_source_dir(), go.full_path(), 'test',
  ] + go_mod_args + global_tag_args + [
    './...',
  ],
  env : goenv,
  timeout: 600,
  suite : 'go',
)

snapdstatedir = get_option('localstatedir') / 'lib' / 'snapd'

install_emptydir(snapdstatedir / 'apparmor/profiles')
install_emptydir(snapdstatedir / 'apparmor/snap-confine')
install_emptydir(snapdstatedir / 'assertions')
install_emptydir(snapdstatedir / 'cache')
install_emptydir(snapdstatedir / 'cgroup')
install_emptydir(snapdstatedir / 'cookie')
install_emptydir(snapdstatedir / 'dbus-1/services')
install_emptydir(snapdstatedir / 'dbus-1/system-services')
install_emptydir(snapdstatedir / 'desktop/applications')
install_emptydir(snapdstatedir / 'device')
install_emptydir(snapdstatedir / 'environment')
install_emptydir(snapdstatedir / 'hostfs')
install_emptydir(snapdstatedir / 'inhibit')
install_emptydir(snapdstatedir / 'lib/gl')
install_emptydir(snapdstatedir / 'lib/gl32')
install_emptydir(snapdstatedir / 'lib/glvnd')
install_emptydir(snapdstatedir / 'lib/vulkan')
install_emptydir(snapdstatedir / 'mount')
install_emptydir(snapdstatedir / 'seccomp/bpf')
install_emptydir(snapdstatedir / 'sequence')
install_emptydir(snapdstatedir / 'snaps')

install_emptydir(snapmountdir / 'bin')
install_emptydir(get_option('localstatedir') / 'cache/snapd')
install_emptydir(get_option('localstatedir') / 'snap')

m4 = find_program('m4', required : true)

subdir('cmd')
subdir('data')

# install_symlink broken in older meson
install_emptydir(get_option('bindir'))
meson.add_install_script('sh', '-c', 'ln -sfr "${DESTDIR}@0@" "${DESTDIR}@1@"'.format(get_option('prefix') / get_option('libexecdir') / 'snapctl', get_option('prefix') / get_option('bindir') / 'snapctl'))
if get_option('selinux').disabled() or (get_option('selinux').auto() and not libselinux.found())
  meson.add_install_script('sh', '-c', 'ln -sfr "${DESTDIR}@0@" "${DESTDIR}@1@"'.format(get_option('prefix') / get_option('libexecdir') / 'snapd', get_option('prefix') / get_option('bindir') / 'snap'))
endif

install_symlink(
  'snap-exec',
  install_dir : get_option('libexecdir'),
  pointing_to : 'snapctl',
)

if get_option('core')
  install_symlink(
    'snap-preseed',
    install_dir : get_option('libexecdir'),
    pointing_to : 'snapd-tool-wrap',
  )
  install_symlink(
    'snap-gpio-helper',
    install_dir : get_option('libexecdir'),
    pointing_to : 'snapd-tool-wrap',
  )
  install_symlink(
    'snap-recovery-chooser',
    install_dir : get_option('libexecdir'),
    pointing_to : 'snapd-tool-wrap',
  )
  install_symlink(
    'snap-fde-keymgr',
    install_dir : get_option('libexecdir'),
    pointing_to : 'snapd-tool-wrap',
  )
endif

# FIXME: is this really going to work when we do not have libapparmor for snap-confine?
install_symlink(
  'snapd-apparmor',
  install_dir : get_option('libexecdir'),
  pointing_to : 'snapd-tool-wrap',
)

if get_option('snap-symlink')
  install_symlink(
    'snap',
    install_dir : '/',
    pointing_to : snapmountdir,
  )
endif

if get_option('setcap')
  meson.add_install_script('setcap.sh')
endif

fmts = custom_target(
  'fmts',
  build_by_default: true,
  output: 'fmts.txt',
  env : goenv,
  capture : true,
  command: [
    env, '-C', meson.current_source_dir(), go.full_path(), 'run',
  ] + go_mod_args + tag_args + [
    './asserts/info',
  ]
)

info_version = configure_file(
  input : 'info.version.in',
  output : 'info.version',
  configuration : configuration_data({
    'version' : build_version,
  })
)

custom_target(
  'info',
  input : [
    info_version,
    fmts,
  ],
  output : 'info',
  command : ['cat', '@INPUT@'],
  capture : true,
  install : true,
  install_dir : get_option('libexecdir'),
)

install_data(
  'NEWS.md',
  install_dir : get_option('datadir') / 'doc/snapd'
)

subdir('checks')
