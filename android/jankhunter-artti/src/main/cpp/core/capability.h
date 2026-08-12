#ifndef JANKHUNTER_ARTTI_CORE_CAPABILITY_H_
#define JANKHUNTER_ARTTI_CORE_CAPABILITY_H_

#include <cstdint>

namespace jankhunter::artti {

enum class Capability : std::uint64_t {
  kGcEvents = 1ULL << 0U,
  kThreadEvents = 1ULL << 1U,
  kMonitorEvents = 1ULL << 2U,
  kStackTrace = 1ULL << 3U,
  kThreadMetadata = 1ULL << 4U,
  kLinuxTid = 1ULL << 5U,
};

class CapabilitySet final {
 public:
  constexpr CapabilitySet() noexcept = default;
  explicit constexpr CapabilitySet(std::uint64_t bits) noexcept : bits_(bits) {}

  [[nodiscard]] constexpr bool contains(Capability value) const noexcept {
    return (bits_ & static_cast<std::uint64_t>(value)) != 0U;
  }

  constexpr void add(Capability value) noexcept { bits_ |= static_cast<std::uint64_t>(value); }
  constexpr void remove(Capability value) noexcept { bits_ &= ~static_cast<std::uint64_t>(value); }

  [[nodiscard]] constexpr std::uint64_t bits() const noexcept { return bits_; }
  [[nodiscard]] constexpr CapabilitySet intersect(CapabilitySet other) const noexcept {
    return CapabilitySet(bits_ & other.bits_);
  }

 private:
  std::uint64_t bits_{0U};
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_CAPABILITY_H_
