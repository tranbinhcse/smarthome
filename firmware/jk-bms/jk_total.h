// Gộp số đo của các pack JK nối song song (dùng trong lambda của jk-total.yaml, jk-pack*.yaml).
// Pack mất kết nối có giá trị NAN và bị bỏ qua; mọi pack NAN thì kết quả NAN ("chưa biết").
// Namespace riêng vì id ESPHome (vd jk_soc) thành biến C++ toàn cục; tránh tên min/max vì Arduino có macro trùng.
// Test: jk_total_test.cpp (go test -run TestJKTotalCpp).
#pragma once
#include <cmath>
#include <cstdint>
#include <initializer_list>

namespace jkt {

inline float count(std::initializer_list<float> v) {
  int n = 0;
  for (float x : v) n += !std::isnan(x);
  return n;
}

// Công suất, dòng: cộng dồn, giữ dấu (+ sạc, − xả).
inline float sum(std::initializer_list<float> v) {
  float s = 0;
  for (float x : v)
    if (!std::isnan(x)) s += x;
  return count(v) ? s : NAN;
}

inline float mean(std::initializer_list<float> v) { return count(v) ? sum(v) / count(v) : NAN; }

inline float smallest(std::initializer_list<float> v) {
  float m = NAN;
  for (float x : v)
    if (!std::isnan(x) && !(x >= m)) m = x;  // so với NAN luôn false nên giá trị hợp lệ đầu tiên được nhận
  return m;
}

inline float largest(std::initializer_list<float> v) {
  float m = NAN;
  for (float x : v)
    if (!std::isnan(x) && !(x <= m)) m = x;
  return m;
}

// SOC chung = Σ(SOC_i × Ah_i) / Σ Ah_i trên các pack có số đo: trung bình theo dung lượng.
// soc và ah cùng thứ tự pack.
inline float soc(std::initializer_list<float> soc, std::initializer_list<float> ah) {
  float num = 0, den = 0;
  const float *a = ah.begin();
  for (float s : soc) {
    if (!std::isnan(s)) {
      num += s * *a;
      den += *a;
    }
    ++a;
  }
  return den > 0 ? num / den : NAN;
}

// Số lỗi đang bật = số bit 1 trong bitmask lỗi của BMS sau khi bỏ các bit ngoài mask.
inline float alarms(uint32_t bits, uint32_t mask) { return __builtin_popcount(bits & mask); }

}  // namespace jkt
