import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:proxy/src/macos_proxy.dart';
import 'package:proxy/src/proxy_command.dart';

class _Commands {
  final calls = <List<String>>[];
  String? failCommand;

  Future<ProcessResult> run(
    String executable,
    List<String> arguments, {
    bool runInShell = false,
  }) async {
    calls.add(arguments);
    if (arguments.first == '-listallnetworkservices') {
      return ProcessResult(1, 0, 'Wi-Fi\n', '');
    }
    return ProcessResult(1, arguments.first == failCommand ? 1 : 0, '', '');
  }

  MacosProxy createProxy() =>
      MacosProxy(commandRunner: ProxyCommandRunner(run));
}

void main() {
  test('shutdown without any proxy setup launches no commands', () async {
    final commands = _Commands();
    final proxy = commands.createProxy();

    expect(await proxy.stop(onlyIfNeeded: true), isTrue);
    expect(commands.calls, isEmpty);
  });

  test(
    'normal stop still cleans up proxy left by an earlier process',
    () async {
      final commands = _Commands();
      final proxy = commands.createProxy();

      expect(await proxy.stop(), isTrue);
      expect(commands.calls, hasLength(6));
      commands.calls.clear();
      expect(await proxy.stop(onlyIfNeeded: true), isTrue);
      expect(commands.calls, isEmpty);
    },
  );

  test('successful stop makes exit cleanup unnecessary', () async {
    final commands = _Commands();
    final proxy = commands.createProxy();

    expect(await proxy.start(7890, []), isTrue);
    expect(await proxy.stop(), isTrue);
    commands.calls.clear();

    expect(await proxy.stop(onlyIfNeeded: true), isTrue);
    expect(commands.calls, isEmpty);
  });

  test('partial setup failure still requires exit cleanup', () async {
    final commands = _Commands()..failCommand = '-setsecurewebproxy';
    final proxy = commands.createProxy();

    expect(await proxy.start(7890, []), isFalse);
    commands.calls.clear();
    commands.failCommand = null;

    expect(await proxy.stop(onlyIfNeeded: true), isTrue);
    expect(commands.calls, hasLength(6));
    expect(commands.calls.last, ['-setproxybypassdomains', 'Wi-Fi', 'Empty']);
  });

  test('failed startup cleanup is retried on exit', () async {
    final commands = _Commands()..failCommand = '-setwebproxystate';
    final proxy = commands.createProxy();

    expect(await proxy.stop(), isFalse);
    commands.calls.clear();
    commands.failCommand = null;

    expect(await proxy.stop(onlyIfNeeded: true), isTrue);
    expect(commands.calls, hasLength(6));
  });
}
