#ifndef JANKHUNTER_ARTTI_CORE_BOUNDED_MPSC_RING_H_
#define JANKHUNTER_ARTTI_CORE_BOUNDED_MPSC_RING_H_

#include <atomic>
#include <cstddef>
#include <cstdint>
#include <memory>

#include "core/status.h"

namespace jankhunter::artti {

template <typename T>
class BoundedMpscRing final {
 public:
  static constexpr std::uint32_t kMaxCasAttempts = 8U;

  explicit BoundedMpscRing(std::uint32_t capacity) noexcept
      : capacity_(IsPowerOfTwo(capacity) ? capacity : 0U),
        mask_(capacity_ == 0U ? 0U : capacity_ - 1U),
        slots_(capacity_ == 0U ? nullptr : std::make_unique<Slot[]>(capacity_)) {
    for (std::uint64_t index = 0U; index < capacity_; ++index) {
      slots_[index].sequence.store(index, std::memory_order_relaxed);
    }
  }

  BoundedMpscRing(const BoundedMpscRing&) = delete;
  BoundedMpscRing& operator=(const BoundedMpscRing&) = delete;
  BoundedMpscRing(BoundedMpscRing&&) = delete;
  BoundedMpscRing& operator=(BoundedMpscRing&&) = delete;

  [[nodiscard]] bool valid() const noexcept { return slots_ != nullptr && capacity_ >= 2U; }
  [[nodiscard]] std::uint32_t capacity() const noexcept { return capacity_; }

  [[nodiscard]] Status TryPush(const T& value) noexcept {
    if (!valid()) return Status::Error(StatusCode::kInvalidState);
    auto position = enqueue_position_.load(std::memory_order_relaxed);
    for (std::uint32_t attempt = 0U; attempt < kMaxCasAttempts; ++attempt) {
      Slot& slot = slots_[Index(position)];
      const auto sequence = slot.sequence.load(std::memory_order_acquire);
      const auto difference = static_cast<std::int64_t>(sequence - position);
      if (difference == 0) {
        if (enqueue_position_.compare_exchange_weak(
                position,
                position + 1U,
                std::memory_order_relaxed,
                std::memory_order_relaxed)) {
          slot.value = value;
          slot.sequence.store(position + 1U, std::memory_order_release);
          return Status::Ok();
        }
      } else if (difference < 0) {
        return Status::Error(StatusCode::kQueueFull);
      } else {
        position = enqueue_position_.load(std::memory_order_relaxed);
      }
    }
    return Status::Error(StatusCode::kContended);
  }

  [[nodiscard]] Status TryPop(T* output) noexcept {
    if (output == nullptr) return Status::Error(StatusCode::kInvalidArgument);
    if (!valid()) return Status::Error(StatusCode::kInvalidState);
    const auto position = dequeue_position_;
    Slot& slot = slots_[Index(position)];
    const auto sequence = slot.sequence.load(std::memory_order_acquire);
    const auto difference = static_cast<std::int64_t>(sequence - (position + 1U));
    if (difference != 0) return Status::Error(StatusCode::kNotFound);
    *output = slot.value;
    slot.sequence.store(position + capacity_, std::memory_order_release);
    dequeue_position_ = position + 1U;
    dequeue_published_.store(dequeue_position_, std::memory_order_release);
    return Status::Ok();
  }

  [[nodiscard]] std::uint64_t ApproximateSize() const noexcept {
    const auto producer = enqueue_position_.load(std::memory_order_acquire);
    const auto consumer = dequeue_published_.load(std::memory_order_acquire);
    const auto difference = producer - consumer;
    return difference > capacity_ ? capacity_ : difference;
  }

  [[nodiscard]] std::size_t MemoryBytes() const noexcept {
    return static_cast<std::size_t>(capacity_) * sizeof(Slot);
  }

 private:
  struct alignas(64) Slot final {
    std::atomic<std::uint64_t> sequence{0U};
    T value{};
  };

  [[nodiscard]] static constexpr bool IsPowerOfTwo(std::uint32_t value) noexcept {
    return value >= 2U && (value & (value - 1U)) == 0U;
  }

  [[nodiscard]] std::uint32_t Index(std::uint64_t position) const noexcept {
    return static_cast<std::uint32_t>(position) & mask_;
  }

  const std::uint32_t capacity_;
  const std::uint32_t mask_;
  std::unique_ptr<Slot[]> slots_;
  alignas(64) std::atomic<std::uint64_t> enqueue_position_{0U};
  alignas(64) std::uint64_t dequeue_position_{0U};
  std::atomic<std::uint64_t> dequeue_published_{0U};
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_BOUNDED_MPSC_RING_H_
