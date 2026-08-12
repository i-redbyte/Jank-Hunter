#ifndef JANKHUNTER_ARTTI_CORE_INTERVAL_TRACKER_H_
#define JANKHUNTER_ARTTI_CORE_INTERVAL_TRACKER_H_

#include <atomic>
#include <cstddef>
#include <cstdint>
#include <memory>

#include "core/event.h"
#include "core/ids.h"
#include "core/quality.h"
#include "core/status.h"

namespace jankhunter::artti {

class GcIntervalTracker final {
 public:
  [[nodiscard]] Status Start(std::uint64_t timestamp_ns, QualityCounters* quality) noexcept;
  [[nodiscard]] Status Finish(
      std::uint64_t timestamp_ns, NativeEvent* event, QualityCounters* quality) noexcept;
  [[nodiscard]] bool open() const noexcept {
    return start_ns_.load(std::memory_order_acquire) != 0U;
  }
  void Reset() noexcept { start_ns_.store(0U, std::memory_order_release); }

 private:
  std::atomic<std::uint64_t> start_ns_{0U};
};

class MonitorIntervalTracker final {
 public:
  explicit MonitorIntervalTracker(std::uint32_t capacity) noexcept;

  [[nodiscard]] bool valid() const noexcept { return entries_ != nullptr && capacity_ > 0U; }
  [[nodiscard]] Status Start(
      ThreadToken token, std::uint64_t timestamp_ns, QualityCounters* quality) noexcept;
  [[nodiscard]] Status Finish(
      ThreadToken token,
      std::uint64_t timestamp_ns,
      std::uint64_t min_duration_ns,
      NativeEvent* event,
      QualityCounters* quality) noexcept;
  [[nodiscard]] std::uint64_t Reset() noexcept;
  [[nodiscard]] std::uint64_t open_count() const noexcept {
    return open_count_.load(std::memory_order_relaxed);
  }
  [[nodiscard]] std::size_t MemoryBytes() const noexcept;

 private:
  struct Entry final {
    std::atomic<std::uint64_t> token{0U};
    std::atomic<std::uint64_t> start_ns{0U};
  };

  [[nodiscard]] std::uint32_t InitialIndex(ThreadToken token) const noexcept;

  static constexpr std::uint64_t kEmpty = 0U;
  static constexpr std::uint64_t kTombstone = UINT64_MAX;
  static constexpr std::uint64_t kReserved = UINT64_MAX - 1U;
  static constexpr std::uint32_t kMaxProbe = 16U;

  const std::uint32_t capacity_;
  std::unique_ptr<Entry[]> entries_;
  std::atomic<std::uint64_t> open_count_{0U};
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_INTERVAL_TRACKER_H_
