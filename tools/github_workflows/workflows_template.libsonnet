{
  local platforms = [
    {
      name: 'linux_amd64',
      buildAndTestCommand: 'test --test_output=errors',
      buildJustBinaries: false,
      extension: '',
    },
    {
      name: 'linux_386',
      buildAndTestCommand: 'test --test_output=errors',
      buildJustBinaries: false,
      extension: '',
    },
    {
      name: 'linux_arm',
      buildAndTestCommand: 'build',
      buildJustBinaries: false,
      extension: '',
    },
    {
      name: 'linux_arm64',
      buildAndTestCommand: 'build',
      buildJustBinaries: false,
      extension: '',
    },
    {
      name: 'darwin_amd64',
      buildAndTestCommand: 'build',
      buildJustBinaries: false,
      extension: '',
    },
    {
      name: 'darwin_arm64',
      buildAndTestCommand: 'build',
      buildJustBinaries: false,
      extension: '',
    },
    {
      name: 'freebsd_amd64',
      buildAndTestCommand: 'build',
      // Building '//...' is broken for FreeBSD, because rules_docker
      // doesn't want to initialize properly.
      // TODO(who?): now that rules_docker is removed, this could be revisited
      buildJustBinaries: true,
      extension: '',
    },
    {
      name: 'windows_amd64',
      buildAndTestCommand: 'build',
      // Building '//...' is broken for Windows, because it depends on a C++
      // toolchain.
      buildJustBinaries: true,
      extension: '.exe',
    },
  ],

  local getJobs(binaries, containers, doUpload) = {
    build_and_test: {
      'runs-on': 'ubuntu-latest',
      steps: [
        // TODO: Switch back to l.gcr.io/google/bazel once updated
        // container images get published once again.
        // https://github.com/GoogleCloudPlatform/container-definitions/issues/12037
        {
          name: 'Check out source code',
          uses: 'actions/checkout@v1',
        },
        {
          name: 'Installing Bazel',
          run: 'v=$(cat .bazelversion) && curl -L https://github.com/bazelbuild/bazel/releases/download/${v}/bazel-${v}-linux-x86_64 > ~/bazel && chmod +x ~/bazel && echo ~ >> ${GITHUB_PATH}',
        },
        {
          name: 'Bazel mod tidy',
          run: 'bazel mod tidy',
        },
        {
          name: 'Gazelle',
          run: |||
            find proto -name BUILD.bazel -delete &&
            bazel run //:gazelle &&
            tools/append_proto_write_source_targets.sh
          |||,
        },
        {
          name: 'Buildifier',
          run: 'bazel run //:buildifier_fix',
        },
        {
          name: 'Gofmt',
          run: 'bazel run @cc_mvdan_gofumpt//:gofumpt -- -w -extra $(pwd)',
        },
        {
          name: 'Clang format',
          run: "find . -name '*.proto' -exec bazel run @llvm_toolchain_llvm//:bin/clang-format -- -i {} +",
        },
        {
          name: 'Test style conformance',
          run: 'git add . && git diff --exit-code HEAD --',
        },
        {
          name: 'Golint',
          run: 'bazel run @org_golang_x_lint//golint -- -set_exit_status $(pwd)/...',
        },
      ] + std.flattenArrays([
        [{
          name: platform.name + ': build and test',
          run: ('bazel %s --platforms=@rules_go//go/toolchain:%s ' % [
                  platform.buildAndTestCommand,
                  platform.name,
                ]) + (
            if platform.buildJustBinaries
            then std.join(' ', ['//cmd/' + binary for binary in binaries])
            else '//...'
          ),
        }] + (
          if doUpload
          then std.flattenArrays([
            [
              {
                name: '%s: copy %s' % [platform.name, binary],
                local executable = binary + platform.extension,
                run: 'rm -f %s && bazel run --run_under cp --platforms=@rules_go//go/toolchain:%s //cmd/%s $(pwd)/%s' % [executable, platform.name, binary, executable],
              },
              {
                name: '%s: upload %s' % [platform.name, binary],
                uses: 'actions/upload-artifact@v4',
                with: {
                  name: '%s.%s' % [binary, platform.name],
                  path: binary + platform.extension,
                },
              },
            ]
            for binary in binaries
          ])
          else []
        )
        for platform in platforms
      ]) + (
        if doUpload
        then (
          [
            {
              name: 'Install Docker credentials',
              run: 'echo "${GITHUB_TOKEN}" | docker login ghcr.io -u $ --password-stdin',
              env: {
                GITHUB_TOKEN: '${{ secrets.GITHUB_TOKEN }}',
              },
            },
          ] + [
            {
              name: 'Push container %s' % container,
              run: 'bazel run --stamp //cmd/%s_container_push' % container,
            }
            for container in containers
          ]
        )
        else []
      ),
    },
  },

  getWorkflows(binaries, containers): {
    'main.yaml': {
      name: 'main',
      on: { push: { branches: ['main'] } },
      jobs: getJobs(binaries, containers, true),
    },
    'pull-requests.yaml': {
      name: 'pull-requests',
      on: { pull_request: { branches: ['main'] } },
      jobs: getJobs(binaries, containers, false),
    },
  },
}
