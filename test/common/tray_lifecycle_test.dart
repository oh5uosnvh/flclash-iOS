import 'package:fl_clash/common/tray.dart';
import 'package:fl_clash/enum/enum.dart';
import 'package:fl_clash/l10n/l10n.dart';
import 'package:fl_clash/models/models.dart';
import 'package:fl_clash/providers/providers.dart';
import 'package:flutter/foundation.dart';
import 'package:material_ui/material_ui.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:riverpod/riverpod.dart';
import 'package:tray/tray.dart';

import '../helpers/test_profiles.dart';

const _channel = MethodChannel('tray');

TrayState _trayState({bool isStart = false}) {
  return TrayState(
    mode: Mode.rule,
    port: 7890,
    autoLaunch: false,
    systemProxy: false,
    tunEnable: false,
    isStart: isStart,
    groups: const [],
    selectedMap: const {},
    showNetworkSpeed: false,
    monochromeTrayIcon: false,
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late List<MethodCall> calls;
  late ProviderContainer container;

  setUpAll(() async {
    await AppLocalizations.load(const Locale('en'));
  });

  setUp(() {
    calls = [];
    container = ProviderContainer(
      overrides: [profilesProvider.overrideWith(TestProfiles.new)],
    );
    Tray.instance.resetForTesting();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(_channel, (call) async {
          calls.add(call);
          return true;
        });
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(_channel, null);
    container.dispose();
    debugDefaultTargetPlatformOverride = null;
  });

  for (final platform in [
    TargetPlatform.windows,
    TargetPlatform.macOS,
    TargetPlatform.linux,
  ]) {
    test(
      '$platform shutdown stops later updates from resurrecting the tray',
      () async {
        debugDefaultTargetPlatformOverride = platform;
        final tray = AppTray.forPlatform(
          isMacOS: platform == TargetPlatform.macOS,
          isWindows: platform == TargetPlatform.windows,
        );
        await tray.update(
          trayState: _trayState(),
          traffic: const Traffic(),
          read: container.read,
        );
        expect(calls.where((call) => call.method == 'show'), hasLength(1));

        await tray.shutdown();
        expect(calls.where((call) => call.method == 'hide'), hasLength(1));

        calls.clear();
        await tray.update(
          trayState: _trayState(isStart: true),
          traffic: const Traffic(),
          read: container.read,
        );
        await tray.updateTitle(
          showNetworkSpeed: true,
          isStart: true,
          traffic: const Traffic(),
        );

        expect(calls, isEmpty);
      },
    );
  }
}
