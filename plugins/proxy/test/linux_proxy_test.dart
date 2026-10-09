import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:proxy/src/linux_proxy.dart';
import 'package:proxy/src/proxy_command.dart';

void main() {
  for (final desktop in ['GNOME', 'MATE', 'KDE']) {
    group('$desktop conditional cleanup', () {
      late LinuxProxy proxy;
      late List<List<String>> calls;
      var failCommands = false;
      var checks = 0;

      setUp(() {
        calls = [];
        checks = 0;
        failCommands = false;
        proxy = LinuxProxy(
          executableChecker: (_) async {
            checks++;
            return true;
          },
          commandRunner: ProxyCommandRunner((
            executable,
            arguments, {
            runInShell = false,
          }) async {
            calls.add(arguments);
            return ProcessResult(1, failCommands ? 1 : 0, '', '');
          }),
        );
      });

      Future<bool> start() =>
          proxy.start(7890, [], desktop: desktop, homeDir: '/home/test');

      Future<bool> stop({bool onlyIfNeeded = false}) => proxy.stop(
        onlyIfNeeded: onlyIfNeeded,
        desktop: desktop,
        homeDir: '/home/test',
      );

      test('unused proxy skips commands and backend detection', () async {
        expect(await stop(onlyIfNeeded: true), isTrue);
        expect(calls, isEmpty);
        expect(checks, 0);
      });

      test(
        'normal stop recovers old settings then exit skips cleanup',
        () async {
          expect(await stop(), isTrue);
          expect(calls, isNotEmpty);
          calls.clear();
          checks = 0;
          expect(await stop(onlyIfNeeded: true), isTrue);
          expect(calls, isEmpty);
          expect(checks, 0);
        },
      );

      test('active proxy is cleaned once', () async {
        expect(await start(), isTrue);
        calls.clear();
        expect(await stop(onlyIfNeeded: true), isTrue);
        expect(calls, isNotEmpty);
        calls.clear();
        expect(await stop(onlyIfNeeded: true), isTrue);
        expect(calls, isEmpty);
      });

      test('failed setup still requires cleanup', () async {
        failCommands = true;
        expect(await start(), isFalse);
        calls.clear();
        failCommands = false;
        expect(await stop(onlyIfNeeded: true), isTrue);
        expect(calls, isNotEmpty);
      });

      test('failed cleanup remains eligible for retry', () async {
        failCommands = true;
        expect(await stop(), isFalse);
        calls.clear();
        failCommands = false;
        expect(await stop(onlyIfNeeded: true), isTrue);
        expect(calls, isNotEmpty);
      });
    });
  }
}
