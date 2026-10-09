import 'dart:convert';

import 'package:flutter/foundation.dart';

import 'tray_menu.dart';
import 'tray_spec.dart';

final class EncodedTray {
  EncodedTray({
    required this.icon,
    required this.toolTip,
    required this.menu,
    required this.itemsById,
    required this.brightness,
  });

  final Map<String, Object?> icon;
  final String toolTip;
  final List<Object?> menu;
  final Map<int, TrayMenuItem> itemsById;
  final Brightness? brightness;
  late final String signature = jsonEncode(<String, Object?>{
    'icon': icon,
    'toolTip': toolTip,
    'brightness': brightness?.name,
    'menu': menu,
  });
}

abstract final class TrayCodec {
  static const int firstItemId = 1024;

  static EncodedTray applyMenuUpdates(
    EncodedTray previous,
    List<Map<String, Object?>> updates,
  ) {
    final pending = <String, Map<String, Object?>>{};
    for (final update in updates) {
      pending.putIfAbsent(update['key']! as String, () => {}).addAll(update);
    }
    List<Object?> patch(List<Object?> items) => items.map((value) {
      if (pending.isEmpty) {
        return value;
      }
      final item = value! as Map<String, Object?>;
      final update = pending.remove(item['key']);
      final children = item['items'];
      if (update == null && children is! List<Object?>) {
        return item;
      }
      return <String, Object?>{
        ...item,
        if (update != null)
          for (final field in update.entries)
            if (field.key != 'checked' || item['type'] == 'checkbox')
              field.key: field.value,
        if (children is List<Object?>) 'items': patch(children),
      };
    }).toList();
    return EncodedTray(
      icon: previous.icon,
      toolTip: previous.toolTip,
      brightness: previous.brightness,
      menu: patch(previous.menu),
      itemsById: previous.itemsById,
    );
  }

  static List<Map<String, Object?>>? menuUpdates(
    EncodedTray previous,
    EncodedTray next,
  ) {
    if (!mapEquals(previous.icon, next.icon) ||
        previous.toolTip != next.toolTip) {
      return null;
    }
    final updates = <Map<String, Object?>>[];
    final keys = <Object>{};
    const mutableFields = {
      'label',
      'sublabel',
      'sublabelStyle',
      'enabled',
      'checked',
    };
    bool compare(List<Object?> before, List<Object?> after) {
      if (before.length != after.length) return false;
      for (var i = 0; i < before.length; i++) {
        final oldItem = before[i] as Map<String, Object?>;
        final newItem = after[i] as Map<String, Object?>;
        final key = newItem['key'];
        if (key != null && !keys.add(key)) return false;
        final update = <String, Object?>{};
        for (final field in {...oldItem.keys, ...newItem.keys}) {
          final oldValue = oldItem[field];
          final newValue = newItem[field];
          if (field == 'items') {
            if (oldValue is! List<Object?> ||
                newValue is! List<Object?> ||
                !compare(oldValue, newValue)) {
              return false;
            }
          } else if (oldValue != newValue &&
              !(oldValue is List &&
                  newValue is List &&
                  listEquals(oldValue, newValue))) {
            if (!mutableFields.contains(field) || newItem['key'] == null) {
              return false;
            }
            update[field] = newValue ?? '';
          }
        }
        if (update.isNotEmpty) {
          updates.add({'key': newItem['key'], ...update});
        }
      }
      return true;
    }

    return compare(previous.menu, next.menu) ? updates : null;
  }

  static EncodedTray encode(TraySpec spec) {
    final itemsById = <int, TrayMenuItem>{};
    final menu = _encodeItems(spec.menu, itemsById, _IdAllocator());
    final icon = <String, Object?>{
      'asset': spec.icon.asset,
      'isTemplate': spec.icon.isTemplate,
      'size': spec.icon.size,
      'position': spec.icon.position.name,
    };
    return EncodedTray(
      icon: icon,
      toolTip: spec.toolTip,
      menu: menu,
      itemsById: itemsById,
      brightness: spec.brightness,
    );
  }

  static List<Object?> _encodeItems(
    List<TrayMenuItem> items,
    Map<int, TrayMenuItem> sink,
    _IdAllocator allocator,
  ) {
    return items.map((item) {
      final id = allocator.next();
      sink[id] = item;
      return switch (item) {
        TrayMenuSeparator() => <String, Object?>{'id': id, 'type': 'separator'},
        TrayMenuAction(
          :final label,
          :final key,
          :final enabled,
          :final sublabel,
          :final sublabelStyle,
          :final keepsMenuOpen,
          :final usesCustomView,
          :final keyEquivalent,
          :final keyEquivalentModifiers,
        ) =>
          <String, Object?>{
            'id': id,
            'type': 'action',
            'label': label,
            'key': ?key,
            'enabled': enabled,
            'sublabel': ?sublabel,
            'sublabelStyle': sublabelStyle.name,
            'keepsMenuOpen': keepsMenuOpen,
            if (usesCustomView) 'usesCustomView': true,
            'keyEquivalent': ?keyEquivalent,
            'keyEquivalentModifiers': keyEquivalentModifiers
                .map((modifier) => modifier.name)
                .toList(),
          },
        TrayMenuCheckbox(
          :final label,
          :final key,
          :final enabled,
          :final checked,
          :final sublabel,
          :final sublabelStyle,
          :final keepsMenuOpen,
          :final usesCustomView,
          :final keyEquivalent,
          :final keyEquivalentModifiers,
        ) =>
          <String, Object?>{
            'id': id,
            'type': 'checkbox',
            'label': label,
            'key': ?key,
            'enabled': enabled,
            'checked': checked,
            'sublabel': ?sublabel,
            'sublabelStyle': sublabelStyle.name,
            'keepsMenuOpen': keepsMenuOpen,
            if (usesCustomView) 'usesCustomView': true,
            'keyEquivalent': ?keyEquivalent,
            'keyEquivalentModifiers': keyEquivalentModifiers
                .map((modifier) => modifier.name)
                .toList(),
          },
        TrayMenuSubmenu(
          :final label,
          :final key,
          :final enabled,
          :final items,
          :final sublabel,
          :final sublabelStyle,
          :final usesCustomView,
        ) =>
          <String, Object?>{
            'id': id,
            'type': 'submenu',
            'label': label,
            'key': ?key,
            'enabled': enabled,
            'sublabel': ?sublabel,
            'sublabelStyle': sublabelStyle.name,
            if (usesCustomView) 'usesCustomView': true,
            'items': _encodeItems(items, sink, allocator),
          },
      };
    }).toList();
  }
}

final class _IdAllocator {
  int _next = TrayCodec.firstItemId;

  int next() {
    final id = _next;
    _next = _next + 1;
    return id;
  }
}
