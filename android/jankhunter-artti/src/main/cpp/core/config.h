#ifndef JANKHUNTER_ARTTI_CORE_CONFIG_H_
#define JANKHUNTER_ARTTI_CORE_CONFIG_H_

#include <cstddef>
#include <cstdint>

#include "core/capability.h"
#include "core/status.h"

namespace jankhunter::artti {

enum class AgentProfile : std::uint32_t {
  kOff = 0,
  kLight = 1,
  kCausal = 2,
  kDeep = 3,
  kCustom = 4,
};

struct NativeConfigSnapshot final {
  std::uint32_t struct_size{sizeof(NativeConfigSnapshot)};
  std::uint32_t schema_version{1U};
  AgentProfile profile{AgentProfile::kCausal};
  std::uint32_t transport_capacity{4096U};
  std::uint32_t max_tracked_threads{512U};
  std::uint32_t max_open_contentions{1024U};
  std::uint32_t max_stack_depth{64U};
  std::uint32_t drain_batch_size{256U};
  std::uint64_t min_contention_duration_ns{8'000'000U};
  std::uint64_t config_hash{0U};
  CapabilitySet requested_capabilities{};

  [[nodiscard]] Status Validate() const noexcept {
    if (struct_size < sizeof(NativeConfigSnapshot) || schema_version != 1U) {
      return Status::Error(StatusCode::kInvalidArgument);
    }
    if (!IsPowerOfTwo(transport_capacity) || transport_capacity < 2U ||
        transport_capacity > 65'536U) {
      return Status::Error(StatusCode::kInvalidArgument);
    }
    if (max_tracked_threads == 0U || max_tracked_threads > 16'384U ||
        max_open_contentions == 0U || max_open_contentions > 65'536U ||
        max_stack_depth == 0U || max_stack_depth > 256U || drain_batch_size == 0U ||
        drain_batch_size > transport_capacity) {
      return Status::Error(StatusCode::kInvalidArgument);
    }
    return Status::Ok();
  }

 private:
  [[nodiscard]] static constexpr bool IsPowerOfTwo(std::uint32_t value) noexcept {
    return value != 0U && (value & (value - 1U)) == 0U;
  }
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_CONFIG_H_
