# tray

System tray integration for FlClash on Linux, macOS and Windows.

## API

The plugin owns call ordering, idempotency, call serialization and unchanged-payload suppression.
Callers declare the desired tray state; they never sequence platform calls themselves.

```dart
await Tray.instance.show(
  TraySpec(
    icon: TrayIcon.asset('assets/images/tray/unix/status_1.png', isTemplate: true),
    toolTip: 'FlClash',
    menu: [
      TrayMenuAction(label: 'Show', onSelected: showWindow),
      const TrayMenuSeparator(),
      TrayMenuCheckbox(label: 'TUN', checked: true, onSelected: toggleTun),
      TrayMenuSubmenu(label: 'Proxy', items: proxyItems),
    ],
  ),
);

await Tray.instance.setTitle('↑ 1.2 MB/s');
await Tray.instance.updateMenuItems(const [
  TrayMenuItemUpdate(key: 'proxy-a', sublabel: '42 ms'),
  TrayMenuItemUpdate(key: 'proxy-b', sublabel: 'Timeout'),
]);
await Tray.instance.hide();
```

- `show` creates the tray on first call and reconciles it afterwards. Re-sending a structurally
  identical `TraySpec` performs no platform call, so callbacks may be rebuilt freely.
- `setTitle` is the incremental path for high-frequency text. It is a no-op where
  `capabilities.title` is false, and while no tray is visible.
- `updateMenuItems` applies one or more keyed item changes in one serialized
  platform call and keeps the menu snapshot in sync for later `show` calls.
- `hide` is idempotent and returns native state to "`show` was never called", so a later `show`
  rebuilds the tray from scratch.
- `openMenu` is a no-op where `capabilities.menuControl` is false.

While a Windows menu is open, item updates remain live. A `show` that requires
rebuilding the menu waits until it closes, retaining only the latest requested
menu and any subsequent keyed updates. Clicks continue to use the displayed
menu's callbacks until the replacement is applied. Repeated open requests are
ignored while tracking, and `hide` discards any deferred replacement.

`TrayIcon.asset` names a bundled PNG and follows Flutter's resolution-aware layout: every
`2.0x/`, `3.0x/`, `4.0x/` sibling that exists is loaded too. macOS receives them all as
representations of one `size`-point image; Linux is handed the largest raster on disk and lets the
indicator scale it; Windows loads the path as-is, so point it at a multi-size `.ico` instead.

Menu item ids are assigned by pre-order position, so an unchanged menu serializes identically across
rebuilds and click dispatch stays stable while a menu is open.

Native submenus are populated on first expansion on macOS and Windows. Unopened
groups retain their serialized entries, and keyed updates modify those entries
without creating native rows. Nested groups are deferred independently. Windows
uses `WM_INITMENUPOPUP` for either popup owner. Linux constructs the complete menu
tree before exporting it through AppIndicator, so opening a submenu does not
replace placeholder rows while the host is displaying it.

Proxy group submenus are available on all three desktop platforms. Windows displays
item sublabels in the right-hand text column; Linux appends them in parentheses so
AppIndicator hosts can display the selected proxy and delay results. Keyed updates
preserve the main label when only the sublabel changes. macOS and Windows honor
`keepsMenuOpen`, so delay tests run and update results inside the open submenu.
Windows supports mouse clicks and Enter/Space activation. Its menu rows use system
drawing, with native bitmap icons showing green (`badge`), yellow (`warning`), red
(`destructive`) or gray (`muted`/`secondary`) status dots beside the node name.
Checked nodes use the system checkmark in place of the status dot and retain their
delay text; this also applies during live updates and DPI or theme changes.
Text, checkmarks, selection backgrounds and layout follow the system menu theme.
The icons scale with menu DPI and follow the system foreground in high-contrast
mode. macOS uses colored badges. Linux's labels remain plain text and its menu
closes after an action.

## Events

`Tray.instance.events` is a broadcast stream of `TrayIconActivated`, `TrayMenuRequested` and
`TrayMenuItemSelected`. Per-item `onSelected` callbacks fire before the corresponding stream event.

## Capabilities

`Tray.instance.capabilities` reports what the current platform can do, so callers branch on ability
rather than on `Platform.isX`.

| | macOS | Windows | Linux |
| --- | --- | --- | --- |
| `title` | yes | no | yes |
| `toolTip` | yes | yes | yes |
| `iconEvents` | yes | yes | no |
| `menuControl` | yes | yes | no |

Linux runs on AppIndicator/StatusNotifierItem, where the desktop shell owns the menu. With Ayatana
AppIndicator 0.6.0 or later, activation requests (such as a KDE left click) emit `TrayIconActivated`,
including any available activation timestamp and Wayland token. Older libraries keep menu-only
behavior. GNOME's AppIndicator extension normally opens the menu on a single click when a menu exists.
The Linux `iconEvents` capability remains false because these events are not guaranteed across
libraries and desktop hosts; programmatic menu opening is unsupported.

## Linux requirements

`libayatana-appindicator3-dev`, or `libappindicator3-dev` as a fallback.

## Native menu tests

The standalone CMake tests exercise menu creation, updates before and after
expansion, reopening and cleanup against the native APIs. Linux checks that the
initial export includes all nested submenu rows and opening preserves them.
Windows checks deferred creation and stable command IDs.
Windows also checks system checkmarks versus status icons, live delay and selection
updates, DPI/theme refreshes, persistent-action keyboard filtering and nested
session cleanup. These tests use hidden owners and do not send desktop input;
they verify native state and message handling, not the menu's rendered appearance.
On Windows, run from the repository root after Flutter has prepared its engine:

```sh
cmake -S plugins/tray/test/native -B .dart_tool/tray_menu_tests -A x64
cmake --build .dart_tool/tray_menu_tests --config Debug
ctest --test-dir .dart_tool/tray_menu_tests -C Debug --output-on-failure
```

On Linux, install the development packages for GTK 3, Ayatana AppIndicator and
libdbusmenu GTK 3, plus Xvfb and xauth. Point `FLUTTER_LINUX_DIR` at the Flutter
engine directory containing `libflutter_linux_gtk.so` and `flutter_linux/`:

```sh
cmake -S plugins/tray/test/native -B .dart_tool/tray_menu_tests \
  -DFLUTTER_LINUX_DIR="$FLUTTER_ROOT/bin/cache/artifacts/engine/linux-x64"
cmake --build .dart_tool/tray_menu_tests
ctest --test-dir .dart_tool/tray_menu_tests --output-on-failure
```
