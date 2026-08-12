#ifndef JANKHUNTER_ARTTI_ART_CAPABILITY_NEGOTIATOR_H_
#define JANKHUNTER_ARTTI_ART_CAPABILITY_NEGOTIATOR_H_

#include "core/capability.h"
#include "core/status.h"

namespace jankhunter::artti::art {

class CapabilityControl {
 public:
  virtual ~CapabilityControl() = default;
  [[nodiscard]] virtual Status GetPotential(CapabilitySet* output) noexcept = 0;
  [[nodiscard]] virtual Status Add(CapabilitySet requested) noexcept = 0;
  [[nodiscard]] virtual Status GetGranted(CapabilitySet* output) noexcept = 0;
};

struct CapabilityNegotiation final {
  Status status{};
  CapabilitySet requested{};
  CapabilitySet potential{};
  CapabilitySet attempted{};
  CapabilitySet granted{};
  bool degraded{false};
};

[[nodiscard]] CapabilityNegotiation NegotiateCapabilities(
    CapabilityControl* control, CapabilitySet requested) noexcept;

}  // namespace jankhunter::artti::art

#endif  // JANKHUNTER_ARTTI_ART_CAPABILITY_NEGOTIATOR_H_
