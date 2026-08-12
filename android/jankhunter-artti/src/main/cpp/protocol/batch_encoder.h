#ifndef JANKHUNTER_ARTTI_PROTOCOL_BATCH_ENCODER_H_
#define JANKHUNTER_ARTTI_PROTOCOL_BATCH_ENCODER_H_

#include <cstddef>
#include <cstdint>
#include <span>

#include "core/event.h"
#include "core/status.h"

namespace jankhunter::artti::protocol {

struct BatchEncodeResult final {
  Status status{};
  std::uint32_t bytes_written{0U};
  std::uint32_t records_written{0U};
  std::uint64_t first_sequence{0U};
  std::uint64_t last_sequence{0U};
};

[[nodiscard]] std::size_t RequiredBatchBytes(std::size_t record_count) noexcept;

[[nodiscard]] BatchEncodeResult EncodeBatch(
    std::span<const NativeEvent> events, std::span<std::byte> output) noexcept;

}  // namespace jankhunter::artti::protocol

#endif  // JANKHUNTER_ARTTI_PROTOCOL_BATCH_ENCODER_H_
