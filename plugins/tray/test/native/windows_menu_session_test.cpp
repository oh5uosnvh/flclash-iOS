#include <windows.h>

#include <iostream>
#include <stdexcept>
#include <string>
#include <unordered_set>
#include <vector>

#include "../../windows/tray_menu_session.h"
#include "../../windows/tray_window.h"

namespace {

void Check(bool condition, const char* message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

class SessionFixture {
 public:
  SessionFixture() : owner({}), menu(::CreatePopupMenu()) {
    Check(owner.Create() && menu != nullptr, "cannot create native menu owner");
    Check(::AppendMenuW(menu, MF_STRING, 201, L"Delay test") != FALSE,
          "cannot add persistent action");
    Check(::AppendMenuW(menu, MF_STRING, 202, L"Select proxy") != FALSE,
          "cannot add ordinary action");
  }
  ~SessionFixture() { ::DestroyMenu(menu); }

  void Select(UINT id, UINT flags = 0) {
    ::SendMessageW(owner.hwnd(), WM_MENUSELECT, MAKEWPARAM(id, flags),
                   reinterpret_cast<LPARAM>(menu));
  }

  bool Key(UINT key, UINT message = WM_KEYDOWN, LPARAM data = 1,
           int context = MSGF_MENU) {
    MSG event{};
    event.hwnd = owner.hwnd();
    event.message = message;
    event.wParam = key;
    event.lParam = data;
    return ::CallMsgFilterW(&event, context) != FALSE;
  }

  tray::TrayWindow owner;
  HMENU menu;
  const std::unordered_set<UINT> persistent{201};
};

void TestSessionInput() {
  SessionFixture fixture;
  std::vector<int> selected;
  tray::TrayMenuSession session(
      fixture.owner.hwnd(), fixture.persistent,
      [&](int id) { selected.push_back(id); }, [](HMENU) {});
  fixture.Select(201);
  Check(fixture.Key(VK_RETURN), "Enter would close the delay-test menu");
  Check(fixture.Key(VK_SPACE), "Space would close the delay-test menu");
  Check(selected == std::vector<int>({201, 201}),
        "persistent action not dispatched");
  Check(fixture.Key(VK_RETURN, WM_KEYDOWN, (1LL << 30) | 1),
        "repeated Enter escaped the menu filter");
  Check(selected.size() == 2, "key repeat started a second delay test");

  ::EnableMenuItem(fixture.menu, 201, MF_BYCOMMAND | MF_GRAYED);
  Check(fixture.Key(VK_RETURN), "disabled delay action closed the menu");
  Check(selected.size() == 2, "disabled delay action was dispatched");
  ::EnableMenuItem(fixture.menu, 201, MF_BYCOMMAND | MF_ENABLED);
  Check(!fixture.Key(VK_ESCAPE), "Escape was swallowed");
  Check(!fixture.Key(VK_DOWN), "keyboard navigation was swallowed");
  Check(!fixture.Key(VK_RETURN, WM_KEYUP), "key release was swallowed");
  Check(!fixture.Key(VK_RETURN, WM_KEYDOWN, 1, MSGF_DIALOGBOX),
        "menu hook intercepted a dialog key");
  fixture.Select(202);
  Check(!fixture.Key(VK_RETURN), "ordinary selection could not close the menu");
  fixture.Select(0, MF_POPUP);
  Check(!fixture.Key(VK_RETURN), "submenu opening was swallowed");
  ::SendMessageW(fixture.owner.hwnd(), WM_MENUSELECT, MAKEWPARAM(0, 0xffff), 0);
  Check(!fixture.Key(VK_RETURN), "closed menu kept handling keys");
}

void TestSessionLifetime() {
  SessionFixture fixture;
  int outer_selected = 0;
  int inner_selected = 0;
  int openings = 0;
  {
    tray::TrayMenuSession outer(
        fixture.owner.hwnd(), fixture.persistent,
        [&](int) { ++outer_selected; }, [&](HMENU) { ++openings; });
    fixture.Select(201);
    {
      tray::TrayMenuSession inner(
          fixture.owner.hwnd(), fixture.persistent,
          [&](int) { ++inner_selected; }, [](HMENU) {});
      fixture.Select(201);
      Check(fixture.Key(VK_RETURN),
            "nested session did not intercept activation");
      Check(inner_selected == 1 && outer_selected == 0,
            "nested activation reached more than one session");
    }
    Check(fixture.Key(VK_RETURN), "outer session was not restored");
    Check(outer_selected == 1, "restored session did not dispatch activation");
    ::SendMessageW(fixture.owner.hwnd(), WM_INITMENUPOPUP,
                   reinterpret_cast<WPARAM>(fixture.menu), 0);
    Check(openings == 1, "menu expansion was not forwarded");
    ::SendMessageW(fixture.owner.hwnd(), WM_INITMENUPOPUP,
                   reinterpret_cast<WPARAM>(fixture.menu), MAKELPARAM(0, TRUE));
    Check(openings == 1, "system menu expansion reached tray callback");
  }
  fixture.Select(201);
  Check(!fixture.Key(VK_RETURN), "session destruction left the hook installed");
  ::SendMessageW(fixture.owner.hwnd(), WM_INITMENUPOPUP,
                 reinterpret_cast<WPARAM>(fixture.menu), 0);
  Check(openings == 1, "session destruction left the owner subclass installed");
}

}  // namespace

int main(int argc, char** argv) {
  try {
    Check(argc == 2, "expected a test case name");
    const std::string name = argv[1];
    if (name == "session_input") {
      TestSessionInput();
    } else if (name == "session_lifetime") {
      TestSessionLifetime();
    } else {
      throw std::runtime_error("unknown test case: " + name);
    }
    std::cout << name << " passed\n";
    return 0;
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
