#include "art/capability_negotiator.h"

namespace jankhunter::artti::art {

CapabilityNegotiation NegotiateCapabilities(
    CapabilityControl* const control, const CapabilitySet requested) noexcept {
  CapabilityNegotiation result{};
  result.requested = requested;
  if (control == nullptr) {
    result.status = Status::Error(StatusCode::kInvalidArgument);
    result.degraded = true;
    return result;
  }
  result.status = control->GetPotential(&result.potential);
  if (!result.status.ok()) {
    result.degraded = true;
    return result;
  }
  result.attempted = requested.intersect(result.potential);
  const auto add_status = control->Add(result.attempted);
  const auto granted_status = control->GetGranted(&result.granted);
  result.status = granted_status.ok() ? add_status : granted_status;
  result.degraded = result.granted.intersect(requested).bits() != requested.bits() ||
      !add_status.ok() || !granted_status.ok();
  return result;
}

}  // namespace jankhunter::artti::art
