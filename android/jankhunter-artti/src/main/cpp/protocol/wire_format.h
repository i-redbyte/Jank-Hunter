#ifndef JANKHUNTER_ARTTI_PROTOCOL_WIRE_FORMAT_H_
#define JANKHUNTER_ARTTI_PROTOCOL_WIRE_FORMAT_H_

#include <cstddef>
#include <cstdint>

namespace jankhunter::artti::protocol {

inline constexpr std::uint32_t kAbiVersion = 1U;
inline constexpr std::uint16_t kProtocolVersion = 1U;
inline constexpr std::uint32_t kBatchMagic = 0x424E484AU;  // "JHNB" in little endian bytes.
inline constexpr std::uint16_t kBatchHeaderSize = 32U;
inline constexpr std::uint32_t kRecordSize = 88U;
inline constexpr std::uint32_t kMaxBatchBytes = 256U * 1024U;
inline constexpr std::uint32_t kNativeConfigWireSize = 72U;
inline constexpr std::uint32_t kHandshakeWireSize = 64U;

inline constexpr std::uint64_t kFeatureLengthDelimitedRecords = 1ULL << 0U;
inline constexpr std::uint64_t kFeatureDirectBufferDrain = 1ULL << 1U;
inline constexpr std::uint64_t kFeatureProducerSequence = 1ULL << 2U;
inline constexpr std::uint64_t kFeatureUnknownRecordSkip = 1ULL << 3U;
inline constexpr std::uint64_t kFeatureBits = kFeatureLengthDelimitedRecords |
    kFeatureDirectBufferDrain | kFeatureProducerSequence | kFeatureUnknownRecordSkip;

}  // namespace jankhunter::artti::protocol

#endif  // JANKHUNTER_ARTTI_PROTOCOL_WIRE_FORMAT_H_
