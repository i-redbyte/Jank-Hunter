#ifndef JANKHUNTER_ARTTI_CORE_CLOCK_H_
#define JANKHUNTER_ARTTI_CORE_CLOCK_H_

#include <chrono>
#include <cstdint>

namespace jankhunter::artti {

struct ClockSource final {
  using NowFunction = std::uint64_t (*)(void*) noexcept;

  void* context{nullptr};
  NowFunction now{nullptr};

  [[nodiscard]] std::uint64_t NowNs() const noexcept {
    return now == nullptr ? 0U : now(context);
  }

  [[nodiscard]] static ClockSource Steady() noexcept {
    return ClockSource{nullptr, [](void*) noexcept -> std::uint64_t {
      const auto value = std::chrono::steady_clock::now().time_since_epoch();
      return static_cast<std::uint64_t>(
          std::chrono::duration_cast<std::chrono::nanoseconds>(value).count());
    }};
  }
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_CLOCK_H_
