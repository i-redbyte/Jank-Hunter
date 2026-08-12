#include "bridge/bridge_runtime.h"

#include <algorithm>
#include <limits>
#include <new>

namespace jankhunter::artti::bridge {

BridgeRuntime& BridgeRuntime::Instance() noexcept {
  static BridgeRuntime runtime;
  return runtime;
}

Status BridgeRuntime::Initialize(const ArtTiNativeConfigV1& wire_config) noexcept {
  if (wire_config.struct_size < sizeof(ArtTiNativeConfigV1) || wire_config.schema_version != 1U) {
    return Status::Error(StatusCode::kInvalidArgument);
  }
  const auto config = ConfigFromWire(wire_config);
  if (!config.Validate().ok()) return Status::Error(StatusCode::kInvalidArgument);

  std::lock_guard lock(control_mutex_);
  if (engine_ != nullptr && engine_->state() == EngineState::kActive) {
    return engine_->config().config_hash == config.config_hash
        ? Status::Ok()
        : Status::Error(StatusCode::kInvalidState);
  }
  auto engine = std::unique_ptr<NativeEngine>(
      new (std::nothrow) NativeEngine(config, ClockSource::Steady()));
  auto scratch = std::unique_ptr<NativeEvent[]>(
      new (std::nothrow) NativeEvent[config.drain_batch_size]);
  if (engine == nullptr || scratch == nullptr) return Status::Error(StatusCode::kInternal);
  const auto status = engine->Start();
  if (!status.ok()) return status;
  scratch_capacity_ = config.drain_batch_size;
  scratch_ = std::move(scratch);
  engine_ = std::move(engine);
  callback_engine_.store(engine_.get(), std::memory_order_release);
  return Status::Ok();
}

ArtTiHandshakeV1 BridgeRuntime::Handshake() noexcept {
  std::lock_guard lock(control_mutex_);
  ArtTiHandshakeV1 handshake{};
  handshake.native_event_size = static_cast<std::uint32_t>(sizeof(NativeEvent));
  if (engine_ != nullptr) {
    handshake.native_memory_bytes = engine_->MemoryBytes();
    handshake.config_hash = engine_->config().config_hash;
  }
  return handshake;
}

protocol::BatchEncodeResult BridgeRuntime::Drain(
    const std::span<std::byte> output, const std::uint32_t max_records) noexcept {
  std::lock_guard lock(control_mutex_);
  protocol::BatchEncodeResult result{};
  if (engine_ == nullptr || scratch_ == nullptr) {
    result.status = Status::Error(StatusCode::kInvalidState);
    return result;
  }
  const auto capacity_from_bytes = output.size() < protocol::kBatchHeaderSize
      ? 0U
      : (output.size() - protocol::kBatchHeaderSize) / protocol::kRecordSize;
  const auto record_capacity = std::min<std::size_t>(
      {static_cast<std::size_t>(max_records),
       static_cast<std::size_t>(scratch_capacity_),
       capacity_from_bytes});
  if (output.size() < protocol::kBatchHeaderSize || (max_records > 0U && record_capacity == 0U)) {
    result.status = Status::Error(StatusCode::kBufferTooSmall);
    result.bytes_written = static_cast<std::uint32_t>(protocol::kBatchHeaderSize + protocol::kRecordSize);
    return result;
  }
  const auto drained = engine_->Drain(std::span<NativeEvent>(scratch_.get(), record_capacity));
  return protocol::EncodeBatch(
      std::span<const NativeEvent>(scratch_.get(), drained.count), output);
}

Status BridgeRuntime::Stop() noexcept {
  std::lock_guard lock(control_mutex_);
  if (engine_ == nullptr) return Status::Ok();
  callback_engine_.store(nullptr, std::memory_order_release);
  const auto status = engine_->BeginStop();
  engine_->MarkStopped();
  return status;
}

std::int32_t BridgeRuntime::PublishSynthetic(
    const std::uint16_t event_type, const std::uint32_t count) noexcept {
  if (count > 100'000U || event_type == 0U || event_type > 0x7FFFU) {
    return -static_cast<std::int32_t>(StatusCode::kInvalidArgument);
  }
  NativeEngine* const engine = callback_engine();
  if (engine == nullptr) return -static_cast<std::int32_t>(StatusCode::kInvalidState);
  std::uint32_t accepted = 0U;
  for (std::uint32_t index = 0U; index < count; ++index) {
    NativeEvent event{};
    event.type = static_cast<EventType>(event_type);
    event.payload.status.value0 = index;
    if (engine->Publish(event).ok()) ++accepted;
  }
  if (accepted > static_cast<std::uint32_t>(std::numeric_limits<std::int32_t>::max())) {
    return std::numeric_limits<std::int32_t>::max();
  }
  return static_cast<std::int32_t>(accepted);
}

NativeConfigSnapshot BridgeRuntime::ConfigFromWire(const ArtTiNativeConfigV1& wire) noexcept {
  NativeConfigSnapshot config{};
  config.profile = static_cast<AgentProfile>(wire.profile);
  config.transport_capacity = wire.transport_capacity;
  config.max_tracked_threads = wire.max_tracked_threads;
  config.max_open_contentions = wire.max_open_contentions;
  config.max_stack_depth = wire.max_stack_depth;
  config.max_stack_definitions = wire.max_stack_definitions;
  config.max_method_definitions = wire.max_method_definitions;
  config.drain_batch_size = wire.drain_batch_size;
  config.min_contention_duration_ns = wire.min_contention_duration_ns;
  config.config_hash = wire.config_hash;
  config.requested_capabilities = CapabilitySet(wire.requested_capabilities);
  return config;
}

}  // namespace jankhunter::artti::bridge
