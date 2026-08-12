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
  std::uint32_t max_stack_definitions{1024U};
  std::uint32_t max_method_definitions{4096U};
  std::uint32_t min_stack_trigger_interval_ms{250U};
  std::uint32_t max_stack_samples_per_minute{120U};
  std::uint32_t drain_batch_size{256U};
  std::uint64_t min_contention_duration_ns{8'000'000U};
  std::uint64_t config_hash{0U};
  CapabilitySet requested_capabilities{};

  [[nodiscard]] Status Validate() const noexcept {
    if (struct_size < sizeof(NativeConfigSnapshot) || schema_version != 1U) {
      return Status::Error(StatusCode::kInvalidArgument);
    }
    if (profile < AgentProfile::kOff || profile > AgentProfile::kCustom ||
        (requested_capabilities.bits() & ~kKnownCapabilityBits) != 0U) {
      return Status::Error(StatusCode::kInvalidArgument);
    }
    if (!IsPowerOfTwo(transport_capacity) || transport_capacity < 2U ||
        transport_capacity > 65'536U) {
      return Status::Error(StatusCode::kInvalidArgument);
    }
    if (max_tracked_threads == 0U || max_tracked_threads > 16'384U ||
        max_open_contentions == 0U || max_open_contentions > 65'536U ||
        max_stack_depth == 0U || max_stack_depth > 256U || drain_batch_size == 0U ||
        drain_batch_size > transport_capacity || max_stack_definitions == 0U ||
        max_stack_definitions > 65'536U || max_method_definitions == 0U ||
        max_method_definitions > 262'144U || min_stack_trigger_interval_ms > 60'000U ||
        max_stack_samples_per_minute == 0U || max_stack_samples_per_minute > 10'000U) {
      return Status::Error(StatusCode::kInvalidArgument);
    }
    if ((requested_capabilities.contains(Capability::kStackTrace) ||
         requested_capabilities.contains(Capability::kThreadMetadata) ||
         requested_capabilities.contains(Capability::kLinuxTid)) &&
        !requested_capabilities.contains(Capability::kThreadEvents)) {
      return Status::Error(StatusCode::kInvalidArgument);
    }
    return Status::Ok();
  }

 private:
  static constexpr std::uint64_t kKnownCapabilityBits =
      static_cast<std::uint64_t>(Capability::kGcEvents) |
      static_cast<std::uint64_t>(Capability::kThreadEvents) |
      static_cast<std::uint64_t>(Capability::kMonitorEvents) |
      static_cast<std::uint64_t>(Capability::kStackTrace) |
      static_cast<std::uint64_t>(Capability::kThreadMetadata) |
      static_cast<std::uint64_t>(Capability::kLinuxTid);

  [[nodiscard]] static constexpr bool IsPowerOfTwo(std::uint32_t value) noexcept {
    return value != 0U && (value & (value - 1U)) == 0U;
  }
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_CONFIG_H_
