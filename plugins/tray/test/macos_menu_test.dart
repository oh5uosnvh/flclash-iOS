import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

String _pluginPath(String relativePath) {
  final direct = File(relativePath);
  return direct.existsSync()
      ? direct.absolute.path
      : File('plugins/tray/$relativePath').absolute.path;
}

void main() {
  test(
    'macOS lazily builds proxy submenus and preserves updates and selections',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'tray_menu_test_',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final executable = '${temporary.path}/menu_test';
      final compilation = await Process.run('xcrun', [
        'swiftc',
        '-module-cache-path',
        '${temporary.path}/module-cache',
        _pluginPath('macos/tray/Sources/tray/TrayMenu.swift'),
        _pluginPath('test/native/macos_menu_test.swift'),
        '-o',
        executable,
      ]);
      expect(compilation.exitCode, 0, reason: '${compilation.stderr}');
      final result = await Process.run(executable, const []);
      expect(result.exitCode, 0, reason: '${result.stdout}\n${result.stderr}');
      expect(result.stdout, contains('Native menu tests passed'));
    },
    skip: !Platform.isMacOS,
    timeout: const Timeout(Duration(minutes: 2)),
  );
}
