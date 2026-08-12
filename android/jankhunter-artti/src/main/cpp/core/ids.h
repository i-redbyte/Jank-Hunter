#ifndef JANKHUNTER_ARTTI_CORE_IDS_H_
#define JANKHUNTER_ARTTI_CORE_IDS_H_

#include <cstdint>

namespace jankhunter::artti {

template <typename Tag>
class StrongId final {
 public:
  constexpr StrongId() noexcept = default;
  explicit constexpr StrongId(std::uint64_t value) noexcept : value_(value) {}

  [[nodiscard]] constexpr std::uint64_t value() const noexcept { return value_; }
  [[nodiscard]] constexpr bool valid() const noexcept { return value_ != 0U; }

  friend constexpr bool operator==(StrongId, StrongId) noexcept = default;

 private:
  std::uint64_t value_{0U};
};

struct ThreadTokenTag;
struct ContextTokenTag;
struct ProducerIdTag;
struct StackFingerprintTag;

using ThreadToken = StrongId<ThreadTokenTag>;
using ContextToken = StrongId<ContextTokenTag>;
using ProducerId = StrongId<ProducerIdTag>;
using StackFingerprint = StrongId<StackFingerprintTag>;

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_IDS_H_
