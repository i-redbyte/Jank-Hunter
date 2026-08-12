#ifndef JANKHUNTER_ARTTI_CORE_ENGINE_H_
#define JANKHUNTER_ARTTI_CORE_ENGINE_H_

#include <atomic>
#include <cstddef>
#include <cstdint>
#include <span>

#include "core/bounded_mpsc_ring.h"
#include "core/clock.h"
#include "core/config.h"
#include "core/event.h"
#include "core/interval_tracker.h"
#include "core/quality.h"
#include "core/status.h"
#include "core/thread_registry.h"

namespace jankhunter::artti {

enum class EngineState : std::uint32_t {
  kUninitialized = 0,
  kActive = 1,
  kStopping = 2,
  kStopped = 3,
  kFailedOpen = 4,
};

struct DrainResult final {
  std::uint32_t count{0U};
  std::uint32_t reserved{0U};
  std::uint64_t first_sequence{0U};
  std::uint64_t last_sequence{0U};
};

class NativeEngine final {
 public:
  NativeEngine(const NativeConfigSnapshot& config, ClockSource clock) noexcept;

  NativeEngine(const NativeEngine&) = delete;
  NativeEngine& operator=(const NativeEngine&) = delete;

  [[nodiscard]] Status Start() noexcept;
  [[nodiscard]] Status BeginStop() noexcept;
  void MarkStopped() noexcept;

  [[nodiscard]] Status Publish(NativeEvent event) noexcept;
  [[nodiscard]] Status PublishQualitySnapshot(bool final_after_stop = false) noexcept;
  [[nodiscard]] DrainResult Drain(std::span<NativeEvent> output) noexcept;

  [[nodiscard]] Status OnGcStart() noexcept;
  [[nodiscard]] Status OnGcFinish() noexcept;
  [[nodiscard]] Status OnThreadStart(
      const ThreadMetadata& metadata, ThreadToken* token) noexcept;
  [[nodiscard]] Status OnThreadEnd(ThreadToken token) noexcept;
  [[nodiscard]] Status UpdateThreadMetadata(
      ThreadToken token, const ThreadMetadata& metadata) noexcept;
  [[nodiscard]] Status OnMonitorEnter(ThreadToken token) noexcept;
  [[nodiscard]] Status OnMonitorEntered(ThreadToken token) noexcept;

  [[nodiscard]] EngineState state() const noexcept { return state_.load(std::memory_order_acquire); }
  [[nodiscard]] QualityCounters& quality() noexcept { return quality_; }
  [[nodiscard]] const NativeConfigSnapshot& config() const noexcept { return config_; }
  [[nodiscard]] std::size_t MemoryBytes() const noexcept;

 private:
  [[nodiscard]] Status PublishPrepared(NativeEvent* event) noexcept;

  NativeConfigSnapshot config_;
  ClockSource clock_;
  BoundedMpscRing<NativeEvent> transport_;
  ThreadRegistry threads_;
  GcIntervalTracker gc_intervals_;
  MonitorIntervalTracker monitor_intervals_;
  QualityCounters quality_;
  std::atomic<EngineState> state_{EngineState::kUninitialized};
  std::atomic<std::uint64_t> next_sequence_{1U};
};

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_ENGINE_H_
