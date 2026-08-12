#include "core/interval_tracker.h"

#include <algorithm>

namespace jankhunter::artti {

Status GcIntervalTracker::Start(
    const std::uint64_t timestamp_ns, QualityCounters* const quality) noexcept {
  if (timestamp_ns == 0U) {
    if (quality != nullptr) quality->Add(QualityCounter::kInvalidInput);
    return Status::Error(StatusCode::kInvalidArgument);
  }
  std::uint64_t expected = 0U;
  if (!start_ns_.compare_exchange_strong(
          expected, timestamp_ns, std::memory_order_acq_rel, std::memory_order_acquire)) {
    if (quality != nullptr) quality->Add(QualityCounter::kGcDuplicateStart);
    return Status::Error(StatusCode::kInvalidState);
  }
  return Status::Ok();
}

Status GcIntervalTracker::Finish(
    const std::uint64_t timestamp_ns,
    NativeEvent* const event,
    QualityCounters* const quality) noexcept {
  if (event == nullptr || timestamp_ns == 0U) {
    if (quality != nullptr) quality->Add(QualityCounter::kInvalidInput);
    return Status::Error(StatusCode::kInvalidArgument);
  }
  const auto start_ns = start_ns_.exchange(0U, std::memory_order_acq_rel);
  if (start_ns == 0U) {
    if (quality != nullptr) quality->Add(QualityCounter::kGcOrphanFinish);
    return Status::Error(StatusCode::kNotFound);
  }
  if (timestamp_ns < start_ns) {
    if (quality != nullptr) quality->Add(QualityCounter::kGcInvalidClock);
    return Status::Error(StatusCode::kCorruptInput);
  }
  *event = NativeEvent{};
  event->type = EventType::kGcInterval;
  event->monotonic_ns = timestamp_ns;
  event->payload.interval.start_ns = start_ns;
  event->payload.interval.duration_ns = timestamp_ns - start_ns;
  return Status::Ok();
}

MonitorIntervalTracker::MonitorIntervalTracker(const std::uint32_t capacity) noexcept
    : capacity_(capacity), entries_(capacity == 0U ? nullptr : std::make_unique<Entry[]>(capacity)) {}

Status MonitorIntervalTracker::Start(
    const ThreadToken token,
    const std::uint64_t timestamp_ns,
    QualityCounters* const quality) noexcept {
  const auto value = token.value();
  if (!valid() || !token.valid() || value >= kReserved || timestamp_ns == 0U) {
    if (quality != nullptr) quality->Add(QualityCounter::kInvalidInput);
    return Status::Error(StatusCode::kInvalidArgument);
  }
  const auto probe_count = std::min(capacity_, kMaxProbe);
  const auto initial = InitialIndex(token);
  for (std::uint32_t probe = 0U; probe < probe_count; ++probe) {
    Entry& entry = entries_[(initial + probe) % capacity_];
    auto observed = entry.token.load(std::memory_order_acquire);
    if (observed == value) {
      if (quality != nullptr) quality->Add(QualityCounter::kContentionDuplicateStart);
      return Status::Error(StatusCode::kInvalidState);
    }
    if (observed != kEmpty && observed != kTombstone) continue;
    if (!entry.token.compare_exchange_strong(
            observed, kReserved, std::memory_order_acq_rel, std::memory_order_acquire)) {
      continue;
    }
    entry.start_ns.store(timestamp_ns, std::memory_order_relaxed);
    entry.token.store(value, std::memory_order_release);
    open_count_.fetch_add(1U, std::memory_order_relaxed);
    return Status::Ok();
  }
  if (quality != nullptr) quality->Add(QualityCounter::kContentionCapacityLoss);
  return Status::Error(StatusCode::kCapacityExhausted);
}

Status MonitorIntervalTracker::Finish(
    const ThreadToken token,
    const std::uint64_t timestamp_ns,
    const std::uint64_t min_duration_ns,
    NativeEvent* const event,
    QualityCounters* const quality) noexcept {
  const auto value = token.value();
  if (!valid() || event == nullptr || !token.valid() || value >= kReserved || timestamp_ns == 0U) {
    if (quality != nullptr) quality->Add(QualityCounter::kInvalidInput);
    return Status::Error(StatusCode::kInvalidArgument);
  }
  const auto probe_count = std::min(capacity_, kMaxProbe);
  const auto initial = InitialIndex(token);
  for (std::uint32_t probe = 0U; probe < probe_count; ++probe) {
    Entry& entry = entries_[(initial + probe) % capacity_];
    auto observed = entry.token.load(std::memory_order_acquire);
    if (observed == kEmpty) break;
    if (observed != value) continue;
    if (!entry.token.compare_exchange_strong(
            observed, kReserved, std::memory_order_acq_rel, std::memory_order_acquire)) {
      if (quality != nullptr) quality->Add(QualityCounter::kContentionContended);
      return Status::Error(StatusCode::kContended);
    }
    const auto start_ns = entry.start_ns.load(std::memory_order_relaxed);
    entry.start_ns.store(0U, std::memory_order_relaxed);
    entry.token.store(kTombstone, std::memory_order_release);
    open_count_.fetch_sub(1U, std::memory_order_relaxed);
    if (timestamp_ns < start_ns) {
      if (quality != nullptr) quality->Add(QualityCounter::kContentionInvalidClock);
      return Status::Error(StatusCode::kCorruptInput);
    }
    const auto duration_ns = timestamp_ns - start_ns;
    if (duration_ns < min_duration_ns) return Status::Error(StatusCode::kNotFound);
    *event = NativeEvent{};
    event->type = EventType::kMonitorContentionInterval;
    event->monotonic_ns = timestamp_ns;
    event->thread_token = value;
    event->payload.interval.start_ns = start_ns;
    event->payload.interval.duration_ns = duration_ns;
    return Status::Ok();
  }
  if (quality != nullptr) quality->Add(QualityCounter::kContentionOrphanFinish);
  return Status::Error(StatusCode::kNotFound);
}

std::uint64_t MonitorIntervalTracker::Reset() noexcept {
  if (!valid()) return 0U;
  std::uint64_t incomplete = 0U;
  for (std::uint32_t index = 0U; index < capacity_; ++index) {
    const auto token = entries_[index].token.exchange(kEmpty, std::memory_order_acq_rel);
    if (token != kEmpty && token != kTombstone && token != kReserved) ++incomplete;
    entries_[index].start_ns.store(0U, std::memory_order_relaxed);
  }
  open_count_.store(0U, std::memory_order_relaxed);
  return incomplete;
}

std::size_t MonitorIntervalTracker::MemoryBytes() const noexcept {
  return static_cast<std::size_t>(capacity_) * sizeof(Entry);
}

std::uint32_t MonitorIntervalTracker::InitialIndex(const ThreadToken token) const noexcept {
  const auto value = token.value();
  const auto mixed = value ^ (value >> 33U) ^ (value << 11U);
  return static_cast<std::uint32_t>(mixed % capacity_);
}

}  // namespace jankhunter::artti
