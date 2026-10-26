// Test hàm gộp pack của jk_total.h, chạy trên máy dev: go test -run TestJKTotalCpp
// (hoặc: c++ -std=c++17 jk_total_test.cpp && ./a.out)
#include "jk_total.h"

#include <cmath>
#include <cstdio>
#include <cstdlib>

static int fails = 0;
static void eq(const char *what, float got, float want) {
  bool ok = std::isnan(want) ? std::isnan(got) : std::fabs(got - want) < 1e-3f;
  if (!ok) {
    std::printf("SAI %s: được %g, cần %g\n", what, got, want);
    fails++;
  }
}

int main() {
  // SOC chung = Σ(SOC × Ah) / Σ Ah: dung lượng bằng nhau thì là trung bình cộng.
  eq("soc bằng dung lượng", jkt::soc({80, 90, 100}, {280, 280, 280}), 90);
  eq("soc khác dung lượng", jkt::soc({50, 100, 100}, {200, 100, 100}), 75);
  // Pack mất kết nối (NAN) bị bỏ khỏi cả tử lẫn mẫu, không kéo SOC về 0.
  eq("soc thiếu một pack", jkt::soc({NAN, 60, 80}, {280, 280, 280}), 70);
  eq("soc mất hết", jkt::soc({NAN, NAN, NAN}, {280, 280, 280}), NAN);

  // Công suất/dòng cộng dồn, giữ dấu: + sạc, − xả.
  eq("công suất", jkt::sum({500, -200, NAN}), 300);
  eq("công suất mất hết", jkt::sum({NAN, NAN, NAN}), NAN);
  eq("điện áp trung bình", jkt::mean({53, NAN, 54}), 53.5);
  eq("điện áp mất hết", jkt::mean({NAN, NAN, NAN}), NAN);

  eq("cell thấp nhất", jkt::smallest({NAN, 3300, 3280}), 3280);
  eq("nóng nhất", jkt::largest({NAN, 31, 47.5}), 47.5);
  eq("max mất hết", jkt::largest({NAN, NAN}), NAN);
  eq("số pack có số đo", jkt::count({1, NAN, 3}), 2);

  // Số lỗi đang bật = số bit 1 của bitmask, mask bỏ các bit không quan tâm.
  eq("đếm bit lỗi", jkt::alarms(0b1011u, 0xFFFFFFFFu), 3);
  eq("mask bỏ bit 0", jkt::alarms(0b1011u, ~1u), 2);

  if (fails) return EXIT_FAILURE;
  std::puts("jk_total: ok");
  return 0;
}
