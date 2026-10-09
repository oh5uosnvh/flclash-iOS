#include "tray_menu_icons.h"

#include <algorithm>
#include <cmath>

namespace tray {

TrayMenuIcons::~TrayMenuIcons() { Clear(); }

bool TrayMenuIcons::SetAppearance(UINT dpi, bool dark) {
  dpi = dpi == 0 ? 96 : dpi;
  HIGHCONTRASTW contrast{};
  contrast.cbSize = sizeof(contrast);
  const bool high_contrast =
      ::SystemParametersInfoW(SPI_GETHIGHCONTRAST, sizeof(contrast), &contrast,
                              0) &&
      (contrast.dwFlags & HCF_HIGHCONTRASTON) != 0;
  const COLORREF foreground = high_contrast ? ::GetSysColor(COLOR_MENUTEXT) : 0;
  if (configured_ && dpi_ == dpi && dark_ == dark &&
      high_contrast_ == high_contrast && foreground_ == foreground) {
    return false;
  }
  dpi_ = dpi;
  dark_ = dark;
  high_contrast_ = high_contrast;
  foreground_ = foreground;
  configured_ = true;
  width_ = std::max<int>(1, ::GetSystemMetricsForDpi(SM_CXMENUCHECK, dpi_));
  height_ = std::max<int>(1, ::GetSystemMetricsForDpi(SM_CYMENUCHECK, dpi_));
  return true;
}

COLORREF TrayMenuIcons::Color(const std::string& style) const {
  if (high_contrast_) {
    return foreground_;
  }
  if (style == "badge") {
    return dark_ ? RGB(102, 187, 106) : RGB(46, 125, 50);
  }
  if (style == "warning") {
    return dark_ ? RGB(255, 202, 40) : RGB(190, 137, 0);
  }
  if (style == "destructive") {
    return dark_ ? RGB(239, 83, 80) : RGB(198, 40, 40);
  }
  return dark_ ? RGB(189, 189, 189) : RGB(117, 117, 117);
}

HBITMAP TrayMenuIcons::Get(const std::string& style) {
  const COLORREF color = Color(style);
  const uint64_t key = (static_cast<uint64_t>(dpi_) << 32) | color;
  const auto cached = bitmaps_.find(key);
  if (cached != bitmaps_.end()) {
    return cached->second;
  }
  BITMAPINFO info{};
  info.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
  info.bmiHeader.biWidth = width_;
  info.bmiHeader.biHeight = -height_;
  info.bmiHeader.biPlanes = 1;
  info.bmiHeader.biBitCount = 32;
  info.bmiHeader.biCompression = BI_RGB;
  void* pixels = nullptr;
  const HBITMAP bitmap =
      ::CreateDIBSection(nullptr, &info, DIB_RGB_COLORS, &pixels, nullptr, 0);
  if (bitmap == nullptr) {
    return nullptr;
  }
  auto* data = static_cast<uint32_t*>(pixels);
  const double radius = std::min<int>(width_, height_) * 0.3;
  for (int y = 0; y < height_; ++y) {
    for (int x = 0; x < width_; ++x) {
      const double dx = x + 0.5 - width_ / 2.0;
      const double dy = y + 0.5 - height_ / 2.0;
      const auto alpha = static_cast<uint32_t>(std::lround(
          std::clamp(radius + 0.5 - std::hypot(dx, dy), 0.0, 1.0) * 255));
      data[y * width_ + x] = (alpha << 24) |
                             ((GetRValue(color) * alpha / 255) << 16) |
                             ((GetGValue(color) * alpha / 255) << 8) |
                             (GetBValue(color) * alpha / 255);
    }
  }
  bitmaps_.emplace(key, bitmap);
  return bitmap;
}

void TrayMenuIcons::Clear() {
  for (const auto& entry : bitmaps_) {
    ::DeleteObject(entry.second);
  }
  bitmaps_.clear();
  configured_ = false;
}

}  // namespace tray
