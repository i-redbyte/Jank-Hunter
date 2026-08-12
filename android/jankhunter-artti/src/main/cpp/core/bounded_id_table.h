#ifndef JANKHUNTER_ARTTI_CORE_BOUNDED_ID_TABLE_H_
#define JANKHUNTER_ARTTI_CORE_BOUNDED_ID_TABLE_H_

#include <algorithm>
#include <cstddef>
#include <cstdint>
#include <memory>
#include <new>

namespace jankhunter::artti {

enum class BoundedIdInsertResult : std::uint8_t { kInserted, kExisting, kFull };

/**
 * Fixed-capacity open-addressed set for non-zero process-local identifiers.
 *
 * Initialization is the only allocating operation. Insert performs at most 32
 * probes, so an adversarial collision pattern becomes an explicit capacity
 * loss instead of unbounded work. The table is intentionally single-threaded:
 * callers serialize control-path stack capture and symbol resolution.
 */
class BoundedIdTable final {
 public:
  [[nodiscard]] bool Initialize(const std::uint32_t capacity) noexcept {
    if (capacity == 0U) return false;
    auto entries = std::unique_ptr<std::uint64_t[]>(new (std::nothrow) std::uint64_t[capacity]{});
    if (entries == nullptr) return false;
    entries_ = std::move(entries);
    capacity_ = capacity;
    return true;
  }

  [[nodiscard]] BoundedIdInsertResult Insert(const std::uint64_t id) noexcept {
    if (entries_ == nullptr || capacity_ == 0U || id == 0U) return BoundedIdInsertResult::kFull;
    const auto initial = static_cast<std::uint32_t>(id % capacity_);
    const auto probes = std::min(capacity_, kMaxProbeCount);
    for (std::uint32_t probe = 0U; probe < probes; ++probe) {
      auto& value = entries_[(initial + probe) % capacity_];
      if (value == id) return BoundedIdInsertResult::kExisting;
      if (value == 0U) {
        value = id;
        return BoundedIdInsertResult::kInserted;
      }
    }
    return BoundedIdInsertResult::kFull;
  }

  [[nodiscard]] std::size_t MemoryBytes() const noexcept {
    return static_cast<std::size_t>(capacity_) * sizeof(std::uint64_t);
  }

  void Reset() noexcept {
    entries_.reset();
    capacity_ = 0U;
  }

 private:
  static constexpr std::uint32_t kMaxProbeCount = 32U;
  std::unique_ptr<std::uint64_t[]> entries_;
  std::uint32_t capacity_{0U};
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_BOUNDED_ID_TABLE_H_
