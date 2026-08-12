#include "protocol/batch_encoder.h"

#include <limits>

#include "protocol/wire_format.h"

namespace jankhunter::artti::protocol {
namespace {

void WriteU16(std::span<std::byte> output, const std::size_t offset, const std::uint16_t value) noexcept {
  output[offset] = static_cast<std::byte>(value & 0xFFU);
  output[offset + 1U] = static_cast<std::byte>((value >> 8U) & 0xFFU);
}

void WriteU32(std::span<std::byte> output, const std::size_t offset, const std::uint32_t value) noexcept {
  for (std::size_t index = 0U; index < sizeof(value); ++index) {
    output[offset + index] = static_cast<std::byte>((value >> (index * 8U)) & 0xFFU);
  }
}

void WriteU64(std::span<std::byte> output, const std::size_t offset, const std::uint64_t value) noexcept {
  for (std::size_t index = 0U; index < sizeof(value); ++index) {
    output[offset + index] = static_cast<std::byte>((value >> (index * 8U)) & 0xFFU);
  }
}

void WriteRecord(std::span<std::byte> output, const NativeEvent& event) noexcept {
  WriteU32(output, 0U, kRecordSize);
  WriteU16(output, 4U, static_cast<std::uint16_t>(event.type));
  WriteU16(output, 6U, event.schema_version);
  WriteU32(output, 8U, event.flags);
  WriteU32(output, 12U, 0U);
  WriteU64(output, 16U, event.producer_sequence);
  WriteU64(output, 24U, event.monotonic_ns);
  WriteU64(output, 32U, event.producer_id);
  WriteU64(output, 40U, event.thread_token);
  WriteU64(output, 48U, event.context_token);
  WriteU64(output, 56U, event.payload.status.value0);
  WriteU64(output, 64U, event.payload.status.value1);
  WriteU64(output, 72U, event.payload.status.value2);
  WriteU64(output, 80U, event.payload.status.value3);
}

}  // namespace

std::size_t RequiredBatchBytes(const std::size_t record_count) noexcept {
  constexpr auto max = std::numeric_limits<std::size_t>::max();
  if (record_count > (max - kBatchHeaderSize) / kRecordSize) return max;
  return static_cast<std::size_t>(kBatchHeaderSize) + record_count * kRecordSize;
}

BatchEncodeResult EncodeBatch(
    const std::span<const NativeEvent> events, const std::span<std::byte> output) noexcept {
  BatchEncodeResult result{};
  if (events.size() > std::numeric_limits<std::uint32_t>::max()) {
    result.status = Status::Error(StatusCode::kInvalidArgument);
    return result;
  }
  const auto required = RequiredBatchBytes(events.size());
  if (required > output.size() || required > kMaxBatchBytes) {
    result.status = Status::Error(StatusCode::kBufferTooSmall);
    result.bytes_written = required > std::numeric_limits<std::uint32_t>::max()
        ? std::numeric_limits<std::uint32_t>::max()
        : static_cast<std::uint32_t>(required);
    return result;
  }

  const auto first_sequence = events.empty() ? 0U : events.front().producer_sequence;
  const auto last_sequence = events.empty() ? 0U : events.back().producer_sequence;
  WriteU32(output, 0U, kBatchMagic);
  WriteU16(output, 4U, kProtocolVersion);
  WriteU16(output, 6U, kBatchHeaderSize);
  WriteU32(output, 8U, static_cast<std::uint32_t>(required));
  WriteU32(output, 12U, static_cast<std::uint32_t>(events.size()));
  WriteU64(output, 16U, first_sequence);
  WriteU64(output, 24U, last_sequence);
  std::size_t offset = kBatchHeaderSize;
  for (const NativeEvent& event : events) {
    WriteRecord(output.subspan(offset, kRecordSize), event);
    offset += kRecordSize;
  }
  result.status = Status::Ok();
  result.bytes_written = static_cast<std::uint32_t>(required);
  result.records_written = static_cast<std::uint32_t>(events.size());
  result.first_sequence = first_sequence;
  result.last_sequence = last_sequence;
  return result;
}

}  // namespace jankhunter::artti::protocol
