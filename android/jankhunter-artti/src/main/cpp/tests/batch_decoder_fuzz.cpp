#include <cstddef>
#include <cstdint>
#include <cstdlib>
#include <span>

#include "protocol/wire_format.h"

namespace {

std::uint16_t ReadU16(const std::span<const std::byte> input, const std::size_t offset) noexcept {
  return static_cast<std::uint16_t>(input[offset]) |
      static_cast<std::uint16_t>(static_cast<std::uint16_t>(input[offset + 1U]) << 8U);
}

std::uint32_t ReadU32(const std::span<const std::byte> input, const std::size_t offset) noexcept {
  std::uint32_t result = 0U;
  for (std::size_t index = 0U; index < sizeof(result); ++index) {
    result |= static_cast<std::uint32_t>(input[offset + index]) << (index * 8U);
  }
  return result;
}

std::uint64_t ReadU64(const std::span<const std::byte> input, const std::size_t offset) noexcept {
  std::uint64_t result = 0U;
  for (std::size_t index = 0U; index < sizeof(result); ++index) {
    result |= static_cast<std::uint64_t>(input[offset + index]) << (index * 8U);
  }
  return result;
}

bool ValidateBatch(const std::span<const std::byte> input) noexcept {
  using namespace jankhunter::artti::protocol;
  if (input.size() < kBatchHeaderSize || input.size() > kMaxBatchBytes) return false;
  if (ReadU32(input, 0U) != kBatchMagic || ReadU16(input, 4U) != kProtocolVersion) return false;
  const auto header_size = ReadU16(input, 6U);
  const auto batch_size = ReadU32(input, 8U);
  const auto record_count = ReadU32(input, 12U);
  if (header_size < kBatchHeaderSize || batch_size < header_size || batch_size > input.size()) return false;
  std::size_t offset = header_size;
  std::uint64_t first = 0U;
  std::uint64_t last = 0U;
  for (std::uint32_t index = 0U; index < record_count; ++index) {
    if (offset > batch_size || batch_size - offset < sizeof(std::uint32_t)) return false;
    const auto size = ReadU32(input, offset);
    if (size < kRecordSize || size > batch_size - offset) return false;
    const auto schema = ReadU16(input, offset + 6U);
    if (schema == 0U) return false;
    const auto sequence = ReadU64(input, offset + 16U);
    if (index == 0U) first = sequence;
    last = sequence;
    offset += size;
  }
  if (offset != batch_size) return false;
  return (record_count == 0U && ReadU64(input, 16U) == 0U && ReadU64(input, 24U) == 0U) ||
      (record_count != 0U && ReadU64(input, 16U) == first && ReadU64(input, 24U) == last);
}

}  // namespace

extern "C" int LLVMFuzzerTestOneInput(const std::uint8_t* data, const std::size_t size) {
  if (data == nullptr && size != 0U) std::abort();
  static_cast<void>(ValidateBatch(std::span<const std::byte>(
      reinterpret_cast<const std::byte*>(data), size)));
  return 0;
}
