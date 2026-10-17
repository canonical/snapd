if get_option('static-checks')
  shellcheck = find_program('shellcheck', required : get_option('shellcheck'))
  clang_format = find_program('clang-format', required : get_option('clang_format'))

  checks = [
    'mdlint.sh',
    'gofmt.sh',
    'equals.sh',
    'http_status.sh',
    'math_rand.sh',
    'ioutil.sh',
    'forbidden_cmd_go_deps.sh',
    'check_mountinfo_override.sh',
    'gopath.sh',
    'missing_interface_spread_test.sh',
    'bad_multiline.sh',
    'bad_match.sh',
    'register_ensure.sh',
    'tests_formatting.sh',
  ]

  if shellcheck.found()
    checks += [
      'shellcheck.sh',
      'shellcheck-spread.sh',
    ]
  endif

  checkenv = environment()
  if all_tags.length() != 0
    checkenv.set('GO_BUILD_TAGS', ' '.join(all_tags))
  endif

  foreach check : checks
    test(check,
      env,
      args : [
        '-C', meson.project_source_root(), meson.current_source_dir() / check,
      ],
      env : checkenv,
      timeout : 600,
      suite : 'static',
    )
  endforeach

  if clang_format.found() and meson.backend() == 'ninja'
    ninja = find_program('ninja')
    test('clang-format',
      ninja,
      args : ['clang-format-check'],
      suite : 'static',
    )
  endif
endif
