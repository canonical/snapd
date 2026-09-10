project(
  'snapd',
  'c',
  version: run_command('version.sh', 'get', check : true).stdout().strip(),
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


conf_data = configuration_data()
conf_data.set('version', meson.project_version())
conf_data.set('project', meson.project_name())
conf_data.set('snapmountdir', snapmountdir)
conf_data.set('snapmountdir_escaped', snapmountdir_escaped.stdout().strip())
conf_data.set('ENABLE_BPF', bpf)
conf_data.set('NVIDIA_BIARCH', get_option('nvidia-biarch'))
conf_data.set('NVIDIA_MULTIARCH', get_option('nvidia-multiarch'))
if get_option('nvidia-biarch')
  conf_data.set('LIB32_DIR', get_option('32bit-libdir'))
  conf_data.set('NATIVE_LIBDIR', get_option('prefix') / get_option('libdir'))
else
  conf_data.set('LIB32_DIR', '')
  conf_data.set('NATIVE_LIBDIR', '')
endif
configure_file(input : 'config.h.in',
               output : 'config.h',
               configuration : conf_data)
configuration_inc = include_directories('.')

add_project_arguments('-DHAVE_CONFIG_H', language : 'c')

snapddir = get_option('prefix') / 'lib/snapd'

goenv = environment()

gopath = get_option('gopath')
if gopath != ''
  goenv.set('GOPATH', gopath)
endif

go = find_program('go', required : true)

goldflags = get_option('goldflags')
if goldflags != ''
  goldflags_args = ['-ldflags=-linkmode external -extldflags "@0@"'.format(goldflags)]
else
  goldflags_args = []
endif

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
    'structuredlogging',
    'faultinject',
    'statelocktrace',
  ]
endif
if get_option('optee')
  global_tags += ['optee']
endif
if get_option('usergo')
  global_tags += ['snapdusergo', 'osusergo']
endif

go_binaries = [
  ['snapd', []],
  ['snapctl', []],
  ['snap-seccomp', []],
  ['snap-update-ns', []],
]

if get_option('core')
  go_binaries += [
    ['snap-bootstrap', ['nomanagers']],
  ]
endif

foreach b : go_binaries
  go_binary = b[0]
  bin_tags = b[1]
  all_tags = global_tags + bin_tags
  if all_tags.length() == 0
    tag_args = []
  else
    tag_args = ['-tags', ' '.join(all_tags)]
  endif
  custom_target(
    go_binary,
    build_by_default: true,
    output: [
      go_binary,
    ],
    install: true,
    install_dir: [
      snapddir,
    ],
    env : goenv,
    command: [
      go, '-C', meson.current_source_dir(), 'build', '-buildmode=pie',
    ] + goldflags_args + go_mod_args + tag_args + [
      '-o', meson.current_build_dir() / go_binary, './cmd' / go_binary,
    ]
  )
endforeach

if global_tags.length() == 0
  global_tag_args = []
else
  global_tag_args = ['-tags', ' '.join(global_tags)]
endif

test('go', go,
  args : [
    '-C', meson.current_source_dir(), 'test',
  ] + go_mod_args + [
    './...',
  ],
  env : goenv,
  timeout: 600,
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

m4 = find_program('m4', required : true)

subdir('cmd')
subdir('data')

# install_symlink broken in older meson
install_emptydir(get_option('bindir'))
meson.add_install_script('sh', '-c', 'ln -sfr "${DESTDIR}@0@" "${DESTDIR}@1@"'.format(snapddir / 'snapctl', get_option('prefix') / get_option('bindir') / 'snapctl'))
meson.add_install_script('sh', '-c', 'ln -sfr "${DESTDIR}@0@" "${DESTDIR}@1@"'.format(snapddir / 'snapd', get_option('prefix') / get_option('bindir') / 'snap'))

install_symlink(
  'snap-exec',
  install_dir : snapddir,
  pointing_to : 'snapctl',
)

install_symlink(
  'snap-preseed',
  install_dir : snapddir,
  pointing_to : 'snapd-tool-wrap',
)

if get_option('apparmor')
  install_symlink(
    'snap-apparmor',
    install_dir : snapddir,
    pointing_to : 'snapd-tool-wrap',
  )
endif

install_symlink(
  'snap-gio-helper',
  install_dir : snapddir,
  pointing_to : 'snapd-tool-wrap',
)

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
    go, '-C', meson.current_source_dir(), 'run',
  ] + go_mod_args + tag_args + [
    './asserts/info',
  ]
)

info_version = configure_file(
  input : 'info.version.in',
  output : 'info.version',
  configuration : configuration_data({
    'version' : meson.project_version()
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
  install_dir : snapddir,
)
