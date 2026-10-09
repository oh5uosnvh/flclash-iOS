#ifndef FLUTTER_PLUGIN_TRAY_MENU_ICONS_H_
#define FLUTTER_PLUGIN_TRAY_MENU_ICONS_H_

#include <windows.h>

#include <cstdint>
#include <string>
#include <unordered_map>

namespace tray {

class TrayMenuIcons {
 public:
  TrayMenuIcons() = default;
  ~TrayMenuIcons();

  TrayMenuIcons(const TrayMenuIcons&) = delete;
  TrayMenuIcons& operator=(const TrayMenuIcons&) = delete;

  bool SetAppearance(UINT dpi, bool dark);
  HBITMAP Get(const std::string& style);
  void Clear();

 private:
  COLORREF Color(const std::string& style) const;

  std::unordered_map<uint64_t, HBITMAP> bitmaps_;
  UINT dpi_ = 96;
  int width_ = 16;
  int height_ = 16;
  bool dark_ = false;
  bool high_contrast_ = false;
  COLORREF foreground_ = 0;
  bool configured_ = false;
};

}  // namespace tray

#endif
