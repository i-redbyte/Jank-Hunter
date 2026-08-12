#ifndef JANKHUNTER_ARTTI_ART_AGENT_OPTIONS_H_
#define JANKHUNTER_ARTTI_ART_AGENT_OPTIONS_H_

#include <cstddef>

#include "bridge/artti_abi.h"
#include "core/status.h"

namespace jankhunter::artti::art {

inline constexpr std::size_t kMaxAgentOptionsLength = 1024U;

[[nodiscard]] Status ParseAgentOptions(
    const char* options, bridge::ArtTiNativeConfigV1* config) noexcept;

}  // namespace jankhunter::artti::art

#endif  // JANKHUNTER_ARTTI_ART_AGENT_OPTIONS_H_
