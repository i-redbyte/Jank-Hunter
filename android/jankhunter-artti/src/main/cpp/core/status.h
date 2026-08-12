#ifndef JANKHUNTER_ARTTI_CORE_STATUS_H_
#define JANKHUNTER_ARTTI_CORE_STATUS_H_

#include <cstdint>

namespace jankhunter::artti {

enum class StatusCode : std::uint32_t {
  kOk = 0,
  kInvalidArgument = 1,
  kInvalidState = 2,
  kQueueFull = 3,
  kContended = 4,
  kCapacityExhausted = 5,
  kNotFound = 6,
  kClosed = 7,
  kBufferTooSmall = 8,
  kCorruptInput = 9,
  kUnsupported = 10,
  kInternal = 11,
};

struct Status final {
  StatusCode code{StatusCode::kOk};

  [[nodiscard]] constexpr bool ok() const noexcept { return code == StatusCode::kOk; }
  [[nodiscard]] static constexpr Status Ok() noexcept { return {}; }
  [[nodiscard]] static constexpr Status Error(StatusCode value) noexcept { return Status{value}; }
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_STATUS_H_
