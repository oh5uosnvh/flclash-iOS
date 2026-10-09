import 'dart:async';

import 'package:fl_clash/common/profile_auto_updater.dart';
import 'package:fl_clash/models/profile.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('fresh imports and re-enabled profiles wait for the next slot', (
    tester,
  ) async {
    final now = tester.binding.clock.now;
    var profiles = <Profile>[];
    var calls = 0;
    final updater = ProfileAutoUpdater(
      profiles: () => profiles,
      now: now,
      update: (_) async => calls++,
      onError: (error) => fail('$error'),
    );
    addTearDown(updater.dispose);
    updater.start();
    await tester.pump(const Duration(minutes: 65));
    profiles = [
      Profile(
        id: 1,
        url: 'https://example.com',
        autoUpdateDuration: const Duration(minutes: 20),
        lastUpdateDate: now(),
      ),
    ];
    updater.reschedule();
    await tester.pump(Duration.zero);
    expect(calls, 0);
    await tester.pump(const Duration(minutes: 15));
    expect(calls, 1);
    profiles = [profiles.single.copyWith(autoUpdate: false)];
    updater.reschedule();
    await tester.pump(const Duration(minutes: 25));
    profiles = [
      profiles.single.copyWith(autoUpdate: true, lastUpdateDate: now()),
    ];
    updater.reschedule();
    await tester.pump(Duration.zero);
    expect(calls, 1);
    await tester.pump(const Duration(minutes: 15));
    expect(calls, 2);
    updater.dispose();
  });

  for (final fails in [false, true]) {
    testWidgets('a batch crossing a slot avoids immediate retries ($fails)', (
      tester,
    ) async {
      final start = tester.binding.clock.now();
      var now = start;
      final calls = <int>[];
      final errors = <Object>[];
      final updater = ProfileAutoUpdater(
        profiles: () => [
          for (final id in [1, 2])
            Profile(
              id: id,
              url: 'https://example.com/$id',
              autoUpdateDuration: const Duration(minutes: 20),
              lastUpdateDate: start,
            ),
        ],
        now: () => now,
        update: (profile) async {
          calls.add(profile.id);
          if (calls.length == 1) {
            now = start.add(const Duration(minutes: 40, seconds: 1));
          }
          if (fails) throw StateError('offline');
        },
        onError: errors.add,
      );
      addTearDown(updater.dispose);
      updater.start();
      now = start.add(const Duration(minutes: 39, seconds: 59));
      await updater.check();
      await tester.pump(Duration.zero);
      await updater.check();
      expect(calls, [1, 2]);
      expect(errors, hasLength(fails ? 2 : 0));
      now = start.add(const Duration(minutes: 60));
      await tester.pump(const Duration(minutes: 19, seconds: 59));
      expect(calls, [1, 2, 1, 2]);
      updater.dispose();
    });
  }

  testWidgets('same intervals align despite different last update times', (
    tester,
  ) async {
    final now = tester.binding.clock.now;
    final calls = <int>[];
    final profiles = [
      Profile(
        id: 1,
        url: 'https://example.com/a',
        autoUpdateDuration: const Duration(minutes: 20),
        lastUpdateDate: now().subtract(const Duration(minutes: 4)),
      ),
      Profile(
        id: 2,
        url: 'https://example.com/b',
        autoUpdateDuration: const Duration(minutes: 20),
        lastUpdateDate: now().subtract(const Duration(minutes: 3)),
      ),
      Profile(
        id: 3,
        url: 'https://example.com/c',
        autoUpdateDuration: const Duration(minutes: 40),
        lastUpdateDate: now(),
      ),
    ];
    final updater = ProfileAutoUpdater(
      profiles: () => profiles,
      now: now,
      update: (profile) async => calls.add(profile.id),
      onError: (error) => fail('$error'),
    );

    updater.start();
    updater.start();
    await tester.pump(const Duration(minutes: 19));
    expect(calls, isEmpty);
    await tester.pump(const Duration(minutes: 1));
    expect(calls, [1, 2]);
    await updater.check();
    expect(calls, [1, 2]);
    await tester.pump(const Duration(minutes: 20));
    expect(calls, [1, 2, 1, 2, 3]);
    updater.dispose();
  });

  testWidgets('startup overdue refresh and failures keep the shared schedule', (
    tester,
  ) async {
    final now = tester.binding.clock.now;
    final calls = <int>[];
    final errors = <Object>[];
    final profiles = [
      const Profile(
        id: 1,
        url: 'https://example.com/a',
        autoUpdateDuration: Duration(minutes: 20),
      ),
      Profile(
        id: 2,
        url: 'https://example.com/b',
        autoUpdateDuration: const Duration(minutes: 20),
        lastUpdateDate: now(),
      ),
    ];
    final updater = ProfileAutoUpdater(
      profiles: () => profiles,
      now: now,
      update: (profile) async {
        calls.add(profile.id);
        if (profile.id == 1) throw StateError('offline');
      },
      onError: errors.add,
    );

    updater.start();
    await tester.pump(Duration.zero);
    expect(calls, [1]);
    await tester.pump(const Duration(minutes: 19));
    expect(calls, [1]);
    await tester.pump(const Duration(minutes: 1));
    expect(calls, [1, 1, 2]);
    expect(errors, hasLength(2));
    updater.dispose();
  });

  testWidgets(
    'profile edits reschedule and disabling all profiles cancels the timer',
    (tester) async {
      final now = tester.binding.clock.now;
      var calls = 0;
      var profiles = [
        Profile(
          id: 1,
          url: 'https://example.com',
          autoUpdateDuration: const Duration(hours: 6),
          lastUpdateDate: now(),
        ),
      ];
      final updater = ProfileAutoUpdater(
        profiles: () => profiles,
        now: now,
        update: (_) async => calls++,
        onError: (error) => fail('$error'),
      );
      updater.start();
      await tester.pump(const Duration(minutes: 10));
      profiles = [
        profiles.single.copyWith(
          autoUpdateDuration: const Duration(minutes: 20),
        ),
      ];
      updater.reschedule();
      await tester.pump(const Duration(minutes: 10));
      expect(calls, 1);
      profiles = [
        profiles.single.copyWith(autoUpdate: false),
        const Profile(id: 2, autoUpdateDuration: Duration.zero),
      ];
      updater.reschedule();
      await tester.pump(const Duration(days: 1));
      expect(calls, 1);
      updater.dispose();
      updater.start();
      await updater.check();
      expect(calls, 1);
    },
  );

  testWidgets('resume catches up once without shifting the startup anchor', (
    tester,
  ) async {
    final start = tester.binding.clock.now();
    var now = start;
    var calls = 0;
    final updater = ProfileAutoUpdater(
      profiles: () => [
        Profile(
          id: 1,
          url: 'https://example.com',
          autoUpdateDuration: const Duration(minutes: 20),
          lastUpdateDate: start,
        ),
      ],
      now: () => now,
      update: (_) async => calls++,
      onError: (error) => fail('$error'),
    );

    updater.start();
    now = start.add(const Duration(minutes: 65));
    await updater.check();
    await updater.check();
    expect(calls, 1);
    now = start.add(const Duration(minutes: 80));
    await tester.pump(const Duration(minutes: 15));
    expect(calls, 2);
    updater.dispose();
  });

  testWidgets('resume cannot overlap an update or rearm after disposal', (
    tester,
  ) async {
    var calls = 0;
    final pending = Completer<void>();
    final updater = ProfileAutoUpdater(
      profiles: () => [
        const Profile(
          id: 1,
          url: 'https://example.com',
          autoUpdateDuration: Duration.zero,
        ),
      ],
      now: tester.binding.clock.now,
      update: (_) {
        calls++;
        return pending.future;
      },
      onError: (error) => fail('$error'),
    );
    updater.start();
    await tester.pump(Duration.zero);
    await updater.check();
    updater.reschedule();
    expect(calls, 1);
    updater.dispose();
    pending.complete();
    await tester.pump(const Duration(days: 1));
    expect(calls, 1);
  });
}
