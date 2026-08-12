#ifndef JANKHUNTER_ARTTI_PROTOCOL_METHOD_WIRE_H_
#define JANKHUNTER_ARTTI_PROTOCOL_METHOD_WIRE_H_

#include <cstdint>

namespace jankhunter::artti::protocol {

inline constexpr std::uint32_t kMethodMagic = 0x444D484AU;  // "JHMD" little endian.
inline constexpr std::uint16_t kMethodProtocolVersion = 1U;
inline constexpr std::uint16_t kMethodHeaderSize = 32U;
inline constexpr std::uint32_t kMaxMethodDefinitionBytes = 4096U;
inline constexpr std::uint32_t kMaxClassSignatureBytes = 1024U;
inline constexpr std::uint32_t kMaxMethodNameBytes = 512U;
inline constexpr std::uint32_t kMaxMethodSignatureBytes = 1024U;

}  // namespace jankhunter::artti::protocol

#endif  // JANKHUNTER_ARTTI_PROTOCOL_METHOD_WIRE_H_
