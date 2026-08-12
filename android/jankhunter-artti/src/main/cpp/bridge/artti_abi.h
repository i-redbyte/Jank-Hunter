#ifndef JANKHUNTER_ARTTI_BRIDGE_ARTTI_ABI_H_
#define JANKHUNTER_ARTTI_BRIDGE_ARTTI_ABI_H_

#include <cstddef>
#include <cstdint>
#include <type_traits>

#include "protocol/wire_format.h"

namespace jankhunter::artti::bridge {

struct ArtTiNativeConfigV1 final {
  std::uint32_t struct_size{sizeof(ArtTiNativeConfigV1)};
  std::uint32_t schema_version{1U};
  std::uint32_t profile{2U};
  std::uint32_t transport_capacity{4096U};
  std::uint32_t max_tracked_threads{512U};
  std::uint32_t max_open_contentions{1024U};
  std::uint32_t max_stack_depth{64U};
  std::uint32_t drain_batch_size{256U};
  std::uint64_t min_contention_duration_ns{8'000'000U};
  std::uint64_t config_hash{0U};
  std::uint64_t requested_capabilities{0U};
  std::uint32_t max_stack_definitions{1024U};
  std::uint32_t max_method_definitions{4096U};
  std::uint64_t reserved1{0U};
};

struct ArtTiHandshakeV1 final {
  std::uint32_t struct_size{sizeof(ArtTiHandshakeV1)};
  std::uint32_t abi_version{protocol::kAbiVersion};
  std::uint32_t protocol_version{protocol::kProtocolVersion};
  std::uint32_t native_event_size{0U};
  std::uint64_t feature_bits{protocol::kFeatureBits};
  std::uint32_t batch_header_size{protocol::kBatchHeaderSize};
  std::uint32_t record_size{protocol::kRecordSize};
  std::uint32_t max_batch_bytes{protocol::kMaxBatchBytes};
  std::uint32_t config_wire_size{protocol::kNativeConfigWireSize};
  std::uint64_t native_memory_bytes{0U};
  std::uint64_t config_hash{0U};
  std::uint64_t reserved{0U};
};

static_assert(sizeof(ArtTiNativeConfigV1) == protocol::kNativeConfigWireSize);
static_assert(sizeof(ArtTiHandshakeV1) == protocol::kHandshakeWireSize);
static_assert(std::is_standard_layout_v<ArtTiNativeConfigV1>);
static_assert(std::is_standard_layout_v<ArtTiHandshakeV1>);

}  // namespace jankhunter::artti::bridge

#endif  // JANKHUNTER_ARTTI_BRIDGE_ARTTI_ABI_H_
