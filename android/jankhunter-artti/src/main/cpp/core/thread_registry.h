#ifndef JANKHUNTER_ARTTI_CORE_THREAD_REGISTRY_H_
#define JANKHUNTER_ARTTI_CORE_THREAD_REGISTRY_H_

#include <atomic>
#include <cstddef>
#include <cstdint>
#include <memory>

#include "core/event.h"
#include "core/ids.h"
#include "core/quality.h"
#include "core/status.h"

namespace jankhunter::artti {

struct ThreadMetadata final {
  std::uint64_t linux_tid{0U};
  std::uint64_t name_id{0U};
  std::uint32_t state{0U};
  std::uint32_t category{0U};
  bool daemon{false};
};

class ThreadRegistry final {
 public:
  explicit ThreadRegistry(std::uint32_t capacity) noexcept;

  [[nodiscard]] bool valid() const noexcept { return entries_ != nullptr && capacity_ > 0U; }
  [[nodiscard]] Status Register(
      const ThreadMetadata& metadata,
      std::uint64_t timestamp_ns,
      ThreadToken* token,
      NativeEvent* event,
      QualityCounters* quality) noexcept;
  [[nodiscard]] Status Update(ThreadToken token, const ThreadMetadata& metadata) noexcept;
  [[nodiscard]] Status Lookup(ThreadToken token, ThreadMetadata* metadata) const noexcept;
  [[nodiscard]] Status Release(
      ThreadToken token,
      std::uint64_t timestamp_ns,
      NativeEvent* event,
      QualityCounters* quality) noexcept;
  [[nodiscard]] std::uint64_t Reset() noexcept;
  [[nodiscard]] std::size_t MemoryBytes() const noexcept;

 private:
  struct Entry final {
    std::atomic<std::uint64_t> token{0U};
    std::atomic<std::uint64_t> linux_tid{0U};
    std::atomic<std::uint64_t> name_id{0U};
    std::atomic<std::uint32_t> state{0U};
    std::atomic<std::uint32_t> category{0U};
    std::atomic<std::uint32_t> daemon{0U};
  };

  [[nodiscard]] std::uint32_t InitialIndex(ThreadToken token) const noexcept;
  [[nodiscard]] Entry* Find(ThreadToken token) noexcept;
  [[nodiscard]] const Entry* Find(ThreadToken token) const noexcept;
  static void StoreMetadata(Entry* entry, const ThreadMetadata& metadata) noexcept;
  static ThreadMetadata LoadMetadata(const Entry& entry) noexcept;
  static void FillThreadEvent(
      NativeEvent* event,
      EventType type,
      ThreadToken token,
      std::uint64_t timestamp_ns,
      const ThreadMetadata& metadata) noexcept;

  static constexpr std::uint64_t kEmpty = 0U;
  static constexpr std::uint64_t kTombstone = UINT64_MAX;
  static constexpr std::uint64_t kReserved = UINT64_MAX - 1U;
  static constexpr std::uint32_t kMaxProbe = 32U;

  const std::uint32_t capacity_;
  std::unique_ptr<Entry[]> entries_;
  std::atomic<std::uint64_t> next_token_{1U};
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_THREAD_REGISTRY_H_
