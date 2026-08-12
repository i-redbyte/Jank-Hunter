#ifndef JANKHUNTER_ARTTI_CORE_COLLECTOR_H_
#define JANKHUNTER_ARTTI_CORE_COLLECTOR_H_

#include <cstdint>

#include "core/capability.h"

namespace jankhunter::artti {

enum class CollectorKind : std::uint32_t {
  kArtGc = 1,
  kArtThread = 2,
  kArtMonitor = 3,
  kArtTriggeredStack = 4,
  kFutureNativeCpu = 100,
  kFutureBatteryEvidence = 200,
};

struct CollectorDescriptor final {
  CollectorKind kind{CollectorKind::kArtGc};
  CapabilitySet required{};
  std::uint32_t schema_version{1U};
  std::uint32_t flags{0U};
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_COLLECTOR_H_
