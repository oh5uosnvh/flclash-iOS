import 'dart:async';

import 'package:fl_clash/models/profile.dart';

class ProfileAutoUpdater {
  ProfileAutoUpdater({
    required this.profiles,
    required this.update,
    required this.onError,
    DateTime Function()? now,
  }) : _now = now ?? DateTime.now;

  final List<Profile> Function() profiles;
  final Future<void> Function(Profile profile) update;
  final void Function(Object error) onError;
  final DateTime Function() _now;
  final _attempts = <int, DateTime>{};
  final _enrolledAt = <int, DateTime>{};
  Timer? _timer;
  DateTime? _startedAt;
  bool _running = false;
  bool _disposed = false;

  void start() {
    if (_disposed || _startedAt != null) return;
    _startedAt = _now();
    reschedule();
  }

  DateTime _due(Profile profile) {
    final interval = profile.autoUpdateDuration > Duration.zero
        ? profile.autoUpdateDuration
        : const Duration(minutes: 20);
    final attempted = _attempts[profile.id];
    final enrolled = _enrolledAt.putIfAbsent(profile.id, _now);
    if (attempted == null &&
        (profile.lastUpdateDate == null ||
            !profile.lastUpdateDate!.add(interval).isAfter(enrolled))) {
      return enrolled;
    }
    final elapsed = (attempted ?? enrolled).difference(_startedAt!);
    final slot = elapsed.inMicroseconds ~/ interval.inMicroseconds + 1;
    return _startedAt!.add(interval * slot);
  }

  void reschedule() {
    if (_disposed || _startedAt == null || _running) return;
    _timer?.cancel();
    _timer = null;
    final enabled = profiles()
        .where((profile) => profile.realAutoUpdate)
        .toList();
    final ids = enabled.map((profile) => profile.id).toSet();
    _attempts.removeWhere((id, _) => !ids.contains(id));
    _enrolledAt.removeWhere((id, _) => !ids.contains(id));
    DateTime? next;
    for (final profile in enabled) {
      final due = _due(profile);
      if (next == null || due.isBefore(next)) next = due;
    }
    if (next != null) {
      _timer = Timer(next.difference(_now()), () => unawaited(check()));
    }
  }

  Future<void> check() async {
    if (_disposed || _startedAt == null || _running) return;
    _timer?.cancel();
    _timer = null;
    _running = true;
    try {
      final ids = profiles().map((profile) => profile.id).toList();
      for (final id in ids) {
        if (_disposed) break;
        final profile = profiles().getProfile(id);
        if (profile == null ||
            !profile.realAutoUpdate ||
            _due(profile).isAfter(_now())) {
          continue;
        }
        try {
          await update(profile);
        } catch (error) {
          onError(error);
        } finally {
          _attempts[id] = _now();
        }
      }
    } finally {
      _running = false;
      reschedule();
    }
  }

  void dispose() {
    _disposed = true;
    _timer?.cancel();
    _timer = null;
  }
}
