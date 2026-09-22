#ifndef JANKHUNTER_ARTTI_CORE_QUALITY_H_
#define JANKHUNTER_ARTTI_CORE_QUALITY_H_

#include <array>
#include <atomic>
#include <cstddef>
#include <cstdint>
#include <limits>

namespace jankhunter::artti {

enum class QualityCounter : std::size_t {
  kPublished = 0,
  kDrained,
  kQueueFull,
  kQueueContended,
  kRejectedAfterClose,
  kGcDuplicateStart,
  kGcOrphanFinish,
  kGcInvalidClock,
  kContentionDuplicateStart,
  kContentionOrphanFinish,
  kContentionCapacityLoss,
  kContentionContended,
  kContentionInvalidClock,
  kThreadCapacityLoss,
  kThreadUnknownEnd,
  kThreadLocalStorageLoss,
  kThreadMetadataLoss,
  kJvmtiError,
  kCallbackAfterStop,
  kStackCaptureFailure,
  kStackDefinitionCapacityLoss,
  kStackDefinitionPublishLoss,
  kMethodResolutionFailure,
  kStackCaptureBudgetLoss,
  kInvalidInput,
  kCount,
};

class QualityCounters final {
 public:
  void Add(QualityCounter counter, std::uint64_t delta = 1U) noexcept {
    auto& target = values_[static_cast<std::size_t>(counter)];
    auto current = target.load(std::memory_order_relaxed);
    while (current != std::numeric_limits<std::uint64_t>::max()) {
      const auto room = std::numeric_limits<std::uint64_t>::max() - current;
      const auto next = delta > room ? std::numeric_limits<std::uint64_t>::max() : current + delta;
      if (target.compare_exchange_weak(
              current, next, std::memory_order_relaxed, std::memory_order_relaxed)) {
        return;
      }
    }
  }

  void ObserveHighWatermark(std::uint64_t value) noexcept {
    auto current = high_watermark_.load(std::memory_order_relaxed);
    while (value > current &&
           !high_watermark_.compare_exchange_weak(
               current, value, std::memory_order_relaxed, std::memory_order_relaxed)) {
    }
  }

  [[nodiscard]] std::uint64_t Get(QualityCounter counter) const noexcept {
    return values_[static_cast<std::size_t>(counter)].load(std::memory_order_relaxed);
  }

  [[nodiscard]] std::uint64_t high_watermark() const noexcept {
    return high_watermark_.load(std::memory_order_relaxed);
  }

  [[nodiscard]] static bool CountsAsActionableNativeLoss(
      const QualityCounter counter) noexcept {
    switch (counter) {
      case QualityCounter::kJvmtiError:
      case QualityCounter::kCallbackAfterStop:
      case QualityCounter::kGcDuplicateStart:
      case QualityCounter::kGcOrphanFinish:
      case QualityCounter::kGcInvalidClock:
      case QualityCounter::kContentionDuplicateStart:
      case QualityCounter::kContentionOrphanFinish:
      case QualityCounter::kContentionContended:
      case QualityCounter::kContentionInvalidClock:
      case QualityCounter::kInvalidInput:
        return false;
      default:
        return true;
    }
  }

  [[nodiscard]] std::uint64_t SumNativeLossExceptQueue() const noexcept {
    std::uint64_t total = 0U;
    for (std::size_t index = static_cast<std::size_t>(QualityCounter::kRejectedAfterClose);
         index < static_cast<std::size_t>(QualityCounter::kCount);
         ++index) {
      const auto counter = static_cast<QualityCounter>(index);
      if (!CountsAsActionableNativeLoss(counter)) {
        continue;
      }
      const auto value = Get(counter);
      const auto room = std::numeric_limits<std::uint64_t>::max() - total;
      total = value > room ? std::numeric_limits<std::uint64_t>::max() : total + value;
    }
    return total;
  }

 private:
  std::array<std::atomic<std::uint64_t>, static_cast<std::size_t>(QualityCounter::kCount)> values_{};
  std::atomic<std::uint64_t> high_watermark_{0U};
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_QUALITY_H_
