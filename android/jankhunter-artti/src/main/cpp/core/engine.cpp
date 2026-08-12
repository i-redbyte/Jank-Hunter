#include "core/engine.h"

namespace jankhunter::artti {

NativeEngine::NativeEngine(const NativeConfigSnapshot& config, const ClockSource clock) noexcept
    : config_(config),
      clock_(clock),
      transport_(config.transport_capacity),
      threads_(config.max_tracked_threads),
      monitor_intervals_(config.max_open_contentions) {}

Status NativeEngine::Start() noexcept {
  if (!config_.Validate().ok() || !transport_.valid() || !threads_.valid() ||
      !monitor_intervals_.valid() || clock_.now == nullptr) {
    state_.store(EngineState::kFailedOpen, std::memory_order_release);
    return Status::Error(StatusCode::kInvalidArgument);
  }
  auto expected = EngineState::kUninitialized;
  if (!state_.compare_exchange_strong(
          expected, EngineState::kActive, std::memory_order_acq_rel, std::memory_order_acquire)) {
    return Status::Error(StatusCode::kInvalidState);
  }
  return Status::Ok();
}

Status NativeEngine::BeginStop() noexcept {
  auto expected = EngineState::kActive;
  if (state_.compare_exchange_strong(
          expected, EngineState::kStopping, std::memory_order_acq_rel, std::memory_order_acquire)) {
    return Status::Ok();
  }
  if (expected == EngineState::kStopping || expected == EngineState::kStopped) return Status::Ok();
  return Status::Error(StatusCode::kInvalidState);
}

void NativeEngine::MarkStopped() noexcept {
  gc_intervals_.Reset();
  const auto incomplete_contentions = monitor_intervals_.Reset();
  if (incomplete_contentions > 0U) {
    quality_.Add(QualityCounter::kContentionOrphanFinish, incomplete_contentions);
  }
  const auto active_threads = threads_.Reset();
  if (active_threads > 0U) quality_.Add(QualityCounter::kThreadUnknownEnd, active_threads);
  state_.store(EngineState::kStopped, std::memory_order_release);
}

void NativeEngine::FailOpen() noexcept {
  state_.store(EngineState::kFailedOpen, std::memory_order_release);
}

Status NativeEngine::Publish(NativeEvent event) noexcept {
  if (event.monotonic_ns == 0U) event.monotonic_ns = clock_.NowNs();
  return PublishPrepared(&event);
}

DrainResult NativeEngine::Drain(const std::span<NativeEvent> output) noexcept {
  DrainResult result{};
  for (NativeEvent& event : output) {
    if (!transport_.TryPop(&event).ok()) break;
    if (result.count == 0U) result.first_sequence = event.producer_sequence;
    result.last_sequence = event.producer_sequence;
    ++result.count;
  }
  if (result.count > 0U) quality_.Add(QualityCounter::kDrained, result.count);
  return result;
}

Status NativeEngine::OnGcStart() noexcept {
  if (state() != EngineState::kActive) return Status::Error(StatusCode::kClosed);
  return gc_intervals_.Start(clock_.NowNs(), &quality_);
}

Status NativeEngine::OnGcFinish() noexcept {
  if (state() != EngineState::kActive) return Status::Error(StatusCode::kClosed);
  NativeEvent event{};
  const auto status = gc_intervals_.Finish(clock_.NowNs(), &event, &quality_);
  return status.ok() ? PublishPrepared(&event) : status;
}

Status NativeEngine::OnThreadStart(
    const ThreadMetadata& metadata, ThreadToken* const token) noexcept {
  if (state() != EngineState::kActive) return Status::Error(StatusCode::kClosed);
  NativeEvent event{};
  const auto status = threads_.Register(metadata, clock_.NowNs(), token, &event, &quality_);
  return status.ok() ? PublishPrepared(&event) : status;
}

Status NativeEngine::OnThreadEnd(const ThreadToken token) noexcept {
  if (state() != EngineState::kActive) return Status::Error(StatusCode::kClosed);
  NativeEvent event{};
  const auto status = threads_.Release(token, clock_.NowNs(), &event, &quality_);
  return status.ok() ? PublishPrepared(&event) : status;
}

Status NativeEngine::OnMonitorEnter(const ThreadToken token) noexcept {
  if (state() != EngineState::kActive) return Status::Error(StatusCode::kClosed);
  return monitor_intervals_.Start(token, clock_.NowNs(), &quality_);
}

Status NativeEngine::OnMonitorEntered(const ThreadToken token) noexcept {
  if (state() != EngineState::kActive) return Status::Error(StatusCode::kClosed);
  NativeEvent event{};
  const auto status = monitor_intervals_.Finish(
      token,
      clock_.NowNs(),
      config_.min_contention_duration_ns,
      &event,
      &quality_);
  return status.ok() ? PublishPrepared(&event) : status;
}

std::size_t NativeEngine::MemoryBytes() const noexcept {
  return sizeof(*this) + transport_.MemoryBytes() + threads_.MemoryBytes() +
      monitor_intervals_.MemoryBytes();
}

Status NativeEngine::PublishPrepared(NativeEvent* const event) noexcept {
  if (event == nullptr) {
    quality_.Add(QualityCounter::kInvalidInput);
    return Status::Error(StatusCode::kInvalidArgument);
  }
  if (state() != EngineState::kActive) {
    quality_.Add(QualityCounter::kRejectedAfterClose);
    return Status::Error(StatusCode::kClosed);
  }
  event->producer_sequence = next_sequence_.fetch_add(1U, std::memory_order_relaxed);
  const auto status = transport_.TryPush(*event);
  if (status.ok()) {
    quality_.Add(QualityCounter::kPublished);
    quality_.ObserveHighWatermark(transport_.ApproximateSize());
  } else if (status.code == StatusCode::kQueueFull) {
    quality_.Add(QualityCounter::kQueueFull);
  } else if (status.code == StatusCode::kContended) {
    quality_.Add(QualityCounter::kQueueContended);
  }
  return status;
}

}  // namespace jankhunter::artti
