#ifndef JANKHUNTER_ARTTI_BRIDGE_BRIDGE_RUNTIME_H_
#define JANKHUNTER_ARTTI_BRIDGE_BRIDGE_RUNTIME_H_

#include <cstddef>
#include <cstdint>
#include <memory>
#include <mutex>
#include <span>

#include "bridge/artti_abi.h"
#include "core/engine.h"
#include "core/status.h"
#include "protocol/batch_encoder.h"

namespace jankhunter::artti::bridge {

class BridgeRuntime final {
 public:
  [[nodiscard]] static BridgeRuntime& Instance() noexcept;

  [[nodiscard]] Status Initialize(const ArtTiNativeConfigV1& wire_config) noexcept;
  [[nodiscard]] ArtTiHandshakeV1 Handshake() noexcept;
  [[nodiscard]] protocol::BatchEncodeResult Drain(
      std::span<std::byte> output, std::uint32_t max_records) noexcept;
  [[nodiscard]] Status Stop() noexcept;
  [[nodiscard]] std::int32_t PublishSynthetic(
      std::uint16_t event_type, std::uint32_t count) noexcept;
  [[nodiscard]] static NativeConfigSnapshot ConfigFromWire(
      const ArtTiNativeConfigV1& wire) noexcept;

  [[nodiscard]] NativeEngine* callback_engine() const noexcept {
    return callback_engine_.load(std::memory_order_acquire);
  }

 private:
  BridgeRuntime() = default;

  mutable std::mutex control_mutex_;
  std::unique_ptr<NativeEngine> engine_;
  std::unique_ptr<NativeEvent[]> scratch_;
  std::uint32_t scratch_capacity_{0U};
  std::uint32_t quality_drain_tick_{0U};
  std::atomic<NativeEngine*> callback_engine_{nullptr};
};

}  // namespace jankhunter::artti::bridge

#endif  // JANKHUNTER_ARTTI_BRIDGE_BRIDGE_RUNTIME_H_
