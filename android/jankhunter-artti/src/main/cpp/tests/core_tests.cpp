#include <array>
#include <atomic>
#include <cstddef>
#include <cstdint>
#include <iostream>
#include <limits>
#include <thread>
#include <vector>

#include "core/bounded_mpsc_ring.h"
#include "core/bounded_id_table.h"
#include "art/agent_options.h"
#include "art/capability_negotiator.h"
#include "core/engine.h"
#include "core/interval_tracker.h"
#include "core/quality.h"
#include "core/thread_registry.h"
#include "protocol/batch_encoder.h"
#include "protocol/wire_format.h"

namespace {

using jankhunter::artti::BoundedMpscRing;
using jankhunter::artti::BoundedIdInsertResult;
using jankhunter::artti::BoundedIdTable;
using jankhunter::artti::ClockSource;
using jankhunter::artti::EngineState;
using jankhunter::artti::EventType;
using jankhunter::artti::GcIntervalTracker;
using jankhunter::artti::MonitorIntervalTracker;
using jankhunter::artti::NativeConfigSnapshot;
using jankhunter::artti::NativeEngine;
using jankhunter::artti::NativeEvent;
using jankhunter::artti::QualityCounter;
using jankhunter::artti::QualityCounters;
using jankhunter::artti::StatusCode;
using jankhunter::artti::ThreadMetadata;
using jankhunter::artti::ThreadRegistry;
using jankhunter::artti::ThreadToken;
using jankhunter::artti::Capability;
using jankhunter::artti::CapabilitySet;
using jankhunter::artti::art::CapabilityControl;
using jankhunter::artti::art::NegotiateCapabilities;
using jankhunter::artti::art::ParseAgentOptions;
using jankhunter::artti::protocol::EncodeBatch;
using jankhunter::artti::protocol::RequiredBatchBytes;

std::atomic<std::uint64_t> test_clock_ns{1U};

std::uint64_t TestNow(void*) noexcept {
  return test_clock_ns.fetch_add(1U, std::memory_order_relaxed);
}

[[noreturn]] void Fail(const char* expression, const char* file, int line) {
  std::cerr << file << ':' << line << ": check failed: " << expression << '\n';
  std::abort();
}

#define JH_CHECK(expression) \
  do { \
    if (!(expression)) Fail(#expression, __FILE__, __LINE__); \
  } while (false)

void ConfigValidation() {
  NativeConfigSnapshot config{};
  JH_CHECK(config.Validate().ok());
  config.transport_capacity = 3U;
  JH_CHECK(config.Validate().code == StatusCode::kInvalidArgument);
  config.transport_capacity = 4096U;
  config.max_stack_depth = 0U;
  JH_CHECK(config.Validate().code == StatusCode::kInvalidArgument);
  config.max_stack_depth = 64U;
  config.requested_capabilities.add(Capability::kStackTrace);
  JH_CHECK(config.Validate().code == StatusCode::kInvalidArgument);
  config.requested_capabilities.add(Capability::kThreadEvents);
  JH_CHECK(config.Validate().ok());
  config.requested_capabilities = CapabilitySet(1ULL << 63U);
  JH_CHECK(config.Validate().code == StatusCode::kInvalidArgument);
  config.requested_capabilities = CapabilitySet{};
  config.profile = static_cast<jankhunter::artti::AgentProfile>(99U);
  JH_CHECK(config.Validate().code == StatusCode::kInvalidArgument);
}

void QueueWrapAndOverflow() {
  BoundedMpscRing<std::uint64_t> queue(4U);
  JH_CHECK(queue.valid());
  for (std::uint64_t value = 1U; value <= 4U; ++value) JH_CHECK(queue.TryPush(value).ok());
  JH_CHECK(queue.TryPush(5U).code == StatusCode::kQueueFull);
  for (std::uint64_t expected = 1U; expected <= 2U; ++expected) {
    std::uint64_t value = 0U;
    JH_CHECK(queue.TryPop(&value).ok());
    JH_CHECK(value == expected);
  }
  JH_CHECK(queue.TryPush(5U).ok());
  JH_CHECK(queue.TryPush(6U).ok());
  for (std::uint64_t expected = 3U; expected <= 6U; ++expected) {
    std::uint64_t value = 0U;
    JH_CHECK(queue.TryPop(&value).ok());
    JH_CHECK(value == expected);
  }
  std::uint64_t value = 0U;
  JH_CHECK(queue.TryPop(&value).code == StatusCode::kNotFound);
}

void BoundedIdDeduplicationAndCollisionPolicy() {
  BoundedIdTable table;
  JH_CHECK(!table.Initialize(0U));
  JH_CHECK(table.Initialize(4U));
  JH_CHECK(table.MemoryBytes() == 4U * sizeof(std::uint64_t));
  JH_CHECK(table.Insert(1U) == BoundedIdInsertResult::kInserted);
  JH_CHECK(table.Insert(1U) == BoundedIdInsertResult::kExisting);
  JH_CHECK(table.Insert(5U) == BoundedIdInsertResult::kInserted);
  JH_CHECK(table.Insert(9U) == BoundedIdInsertResult::kInserted);
  JH_CHECK(table.Insert(13U) == BoundedIdInsertResult::kInserted);
  JH_CHECK(table.Insert(17U) == BoundedIdInsertResult::kFull);
  JH_CHECK(table.Insert(0U) == BoundedIdInsertResult::kFull);
  table.Reset();
  JH_CHECK(table.MemoryBytes() == 0U);
}

void QueueMultiProducerStress() {
  constexpr std::uint32_t kProducerCount = 8U;
  constexpr std::uint32_t kPerProducer = 20'000U;
  constexpr std::uint32_t kCapacity = 1024U;
  BoundedMpscRing<std::uint64_t> queue(kCapacity);
  std::atomic<std::uint32_t> producers_done{0U};
  std::atomic<std::uint64_t> dropped{0U};
  std::vector<std::thread> producers;
  producers.reserve(kProducerCount);
  for (std::uint32_t producer = 0U; producer < kProducerCount; ++producer) {
    producers.emplace_back([producer, &queue, &producers_done, &dropped] {
      for (std::uint32_t sequence = 0U; sequence < kPerProducer; ++sequence) {
        const auto value = (static_cast<std::uint64_t>(producer) << 32U) | sequence;
        if (!queue.TryPush(value).ok()) dropped.fetch_add(1U, std::memory_order_relaxed);
      }
      producers_done.fetch_add(1U, std::memory_order_release);
    });
  }

  std::vector<std::uint8_t> seen(kProducerCount * kPerProducer, 0U);
  std::uint64_t drained = 0U;
  while (producers_done.load(std::memory_order_acquire) != kProducerCount ||
         queue.ApproximateSize() != 0U) {
    std::uint64_t value = 0U;
    if (!queue.TryPop(&value).ok()) {
      std::this_thread::yield();
      continue;
    }
    const auto producer = static_cast<std::uint32_t>(value >> 32U);
    const auto sequence = static_cast<std::uint32_t>(value);
    JH_CHECK(producer < kProducerCount);
    JH_CHECK(sequence < kPerProducer);
    const auto index = static_cast<std::size_t>(producer) * kPerProducer + sequence;
    JH_CHECK(seen[index] == 0U);
    seen[index] = 1U;
    ++drained;
  }
  for (auto& producer : producers) producer.join();
  JH_CHECK(drained + dropped.load(std::memory_order_relaxed) ==
           static_cast<std::uint64_t>(kProducerCount) * kPerProducer);
}

void SaturatingQualityCounters() {
  QualityCounters quality;
  quality.Add(QualityCounter::kPublished, std::numeric_limits<std::uint64_t>::max() - 2U);
  quality.Add(QualityCounter::kPublished, 10U);
  JH_CHECK(quality.Get(QualityCounter::kPublished) == std::numeric_limits<std::uint64_t>::max());
  quality.ObserveHighWatermark(5U);
  quality.ObserveHighWatermark(3U);
  quality.ObserveHighWatermark(9U);
  JH_CHECK(quality.high_watermark() == 9U);
}

void GcPairing() {
  GcIntervalTracker tracker;
  QualityCounters quality;
  NativeEvent event{};
  JH_CHECK(tracker.Start(100U, &quality).ok());
  JH_CHECK(tracker.Start(101U, &quality).code == StatusCode::kInvalidState);
  JH_CHECK(tracker.Finish(160U, &event, &quality).ok());
  JH_CHECK(event.type == EventType::kGcInterval);
  JH_CHECK(event.payload.interval.start_ns == 100U);
  JH_CHECK(event.payload.interval.duration_ns == 60U);
  JH_CHECK(tracker.Finish(170U, &event, &quality).code == StatusCode::kNotFound);
  JH_CHECK(quality.Get(QualityCounter::kGcDuplicateStart) == 1U);
  JH_CHECK(quality.Get(QualityCounter::kGcOrphanFinish) == 1U);
}

void MonitorPairingAndCapacity() {
  MonitorIntervalTracker tracker(2U);
  QualityCounters quality;
  NativeEvent event{};
  JH_CHECK(tracker.Start(ThreadToken(1U), 100U, &quality).ok());
  JH_CHECK(tracker.Start(ThreadToken(2U), 110U, &quality).ok());
  JH_CHECK(tracker.Start(ThreadToken(3U), 120U, &quality).code == StatusCode::kCapacityExhausted);
  JH_CHECK(tracker.Finish(ThreadToken(1U), 105U, 10U, &event, &quality).code == StatusCode::kNotFound);
  JH_CHECK(tracker.Finish(ThreadToken(2U), 150U, 10U, &event, &quality).ok());
  JH_CHECK(event.type == EventType::kMonitorContentionInterval);
  JH_CHECK(event.thread_token == 2U);
  JH_CHECK(event.payload.interval.duration_ns == 40U);
  JH_CHECK(tracker.open_count() == 0U);
  JH_CHECK(quality.Get(QualityCounter::kContentionCapacityLoss) == 1U);
}

void ThreadRegistryLifecycle() {
  ThreadRegistry registry(4U);
  QualityCounters quality;
  NativeEvent event{};
  ThreadToken first;
  ThreadMetadata metadata{42U, 7U, 3U, 2U, true};
  JH_CHECK(registry.Register(metadata, 100U, &first, &event, &quality).ok());
  JH_CHECK(first.valid());
  JH_CHECK(event.type == EventType::kThreadStart);
  ThreadMetadata resolved{};
  JH_CHECK(registry.Lookup(first, &resolved).ok());
  JH_CHECK(resolved.linux_tid == 42U);
  metadata.linux_tid = 43U;
  JH_CHECK(registry.Update(first, metadata).ok());
  JH_CHECK(registry.Release(first, 200U, &event, &quality).ok());
  JH_CHECK(event.type == EventType::kThreadEnd);
  JH_CHECK(event.payload.thread.linux_tid == 43U);
  JH_CHECK(registry.Lookup(first, &resolved).code == StatusCode::kNotFound);
}

void EngineLifecycleAndIntervals() {
  test_clock_ns.store(1U, std::memory_order_relaxed);
  NativeConfigSnapshot config{};
  config.transport_capacity = 8U;
  config.max_tracked_threads = 4U;
  config.max_open_contentions = 4U;
  config.drain_batch_size = 4U;
  config.min_contention_duration_ns = 1U;
  NativeEngine engine(config, ClockSource{nullptr, TestNow});
  JH_CHECK(engine.Start().ok());
  JH_CHECK(engine.state() == EngineState::kActive);
  ThreadToken token;
  JH_CHECK(engine.OnThreadStart(ThreadMetadata{99U, 0U, 0U, 0U, false}, &token).ok());
  JH_CHECK(engine.OnGcStart().ok());
  JH_CHECK(engine.OnGcFinish().ok());
  JH_CHECK(engine.OnMonitorEnter(token).ok());
  JH_CHECK(engine.OnMonitorEntered(token).ok());
  JH_CHECK(engine.OnThreadEnd(token).ok());
  std::array<NativeEvent, 8U> events{};
  const auto drained = engine.Drain(events);
  JH_CHECK(drained.count == 4U);
  JH_CHECK(drained.first_sequence == 1U);
  JH_CHECK(drained.last_sequence == 4U);
  JH_CHECK(engine.BeginStop().ok());
  JH_CHECK(engine.Publish(NativeEvent{}).code == StatusCode::kClosed);
  engine.MarkStopped();
  JH_CHECK(engine.state() == EngineState::kStopped);
  JH_CHECK(engine.quality().Get(QualityCounter::kPublished) == 4U);
  JH_CHECK(engine.quality().Get(QualityCounter::kDrained) == 4U);
  JH_CHECK(engine.quality().Get(QualityCounter::kRejectedAfterClose) == 1U);
}

void EngineStartStopRace() {
  NativeConfigSnapshot config{};
  config.transport_capacity = 256U;
  config.max_tracked_threads = 16U;
  config.max_open_contentions = 16U;
  config.drain_batch_size = 64U;
  NativeEngine engine(config, ClockSource{nullptr, TestNow});
  JH_CHECK(engine.Start().ok());
  std::atomic<bool> run{true};
  std::atomic<std::uint64_t> attempts{0U};
  std::vector<std::thread> producers;
  for (std::uint32_t index = 0U; index < 4U; ++index) {
    producers.emplace_back([&engine, &run, &attempts] {
      while (run.load(std::memory_order_relaxed)) {
        NativeEvent event{};
        event.type = EventType::kClockSync;
        static_cast<void>(engine.Publish(event));
        attempts.fetch_add(1U, std::memory_order_relaxed);
      }
    });
  }
  std::this_thread::sleep_for(std::chrono::milliseconds(10));
  JH_CHECK(engine.BeginStop().ok());
  run.store(false, std::memory_order_relaxed);
  for (auto& producer : producers) producer.join();
  std::array<NativeEvent, 256U> events{};
  static_cast<void>(engine.Drain(events));
  engine.MarkStopped();
  JH_CHECK(attempts.load(std::memory_order_relaxed) > 0U);
  JH_CHECK(engine.state() == EngineState::kStopped);
}

void EngineQualitySnapshotIsBoundedAndCumulative() {
  NativeConfigSnapshot config{};
  config.transport_capacity = 8U;
  config.max_tracked_threads = 2U;
  config.max_open_contentions = 2U;
  config.drain_batch_size = 2U;
  NativeEngine engine(config, ClockSource{nullptr, TestNow});
  JH_CHECK(engine.Start().ok());
  engine.quality().ObserveHighWatermark(7U);
  engine.quality().Add(QualityCounter::kQueueFull, 3U);
  engine.quality().Add(QualityCounter::kQueueContended, 2U);
  engine.quality().Add(QualityCounter::kGcOrphanFinish, 4U);
  JH_CHECK(engine.PublishQualitySnapshot().ok());
  std::array<NativeEvent, 1U> output{};
  JH_CHECK(engine.Drain(output).count == 1U);
  JH_CHECK(output[0].type == EventType::kQualitySnapshot);
  JH_CHECK(output[0].payload.status.value0 == 7U);
  JH_CHECK(output[0].payload.status.value1 == 3U);
  JH_CHECK(output[0].payload.status.value2 == 2U);
  JH_CHECK(output[0].payload.status.value3 == 4U);
  JH_CHECK(output[0].monotonic_ns != 0U);
  JH_CHECK(engine.BeginStop().ok());
  engine.MarkStopped();
  JH_CHECK(engine.PublishQualitySnapshot(true).ok());
  JH_CHECK(engine.Drain(output).count == 1U);
}

std::uint32_t ReadU32(const std::vector<std::byte>& input, const std::size_t offset) {
  std::uint32_t result = 0U;
  for (std::size_t index = 0U; index < sizeof(result); ++index) {
    result |= static_cast<std::uint32_t>(input[offset + index]) << (index * 8U);
  }
  return result;
}

void BatchEncodingBoundsAndEnvelope() {
  std::array<NativeEvent, 2U> events{};
  events[0].type = EventType::kGcInterval;
  events[0].producer_sequence = 7U;
  events[0].monotonic_ns = 100U;
  events[0].payload.interval.start_ns = 90U;
  events[0].payload.interval.duration_ns = 10U;
  events[1].type = EventType::kClockSync;
  events[1].producer_sequence = 9U;
  events[1].monotonic_ns = 110U;
  const auto required = RequiredBatchBytes(events.size());
  JH_CHECK(required == jankhunter::artti::protocol::kBatchHeaderSize +
      events.size() * jankhunter::artti::protocol::kRecordSize);
  std::vector<std::byte> output(required);
  const auto encoded = EncodeBatch(events, output);
  JH_CHECK(encoded.status.ok());
  JH_CHECK(encoded.bytes_written == required);
  JH_CHECK(encoded.records_written == events.size());
  JH_CHECK(encoded.first_sequence == 7U);
  JH_CHECK(encoded.last_sequence == 9U);
  JH_CHECK(ReadU32(output, 0U) == jankhunter::artti::protocol::kBatchMagic);
  JH_CHECK(ReadU32(output, 8U) == required);
  JH_CHECK(ReadU32(output, 12U) == events.size());
  std::vector<std::byte> short_output(required - 1U);
  const auto rejected = EncodeBatch(events, short_output);
  JH_CHECK(rejected.status.code == StatusCode::kBufferTooSmall);
  JH_CHECK(rejected.bytes_written == required);
}

void AgentOptionsAreBounded() {
  jankhunter::artti::bridge::ArtTiNativeConfigV1 config{};
  JH_CHECK(ParseAgentOptions(
      "v=1;profile=3;transport=1024;threads=128;contentions=256;depth=96;stackdefs=512;"
      "methoddefs=2048;triggerms=50;samplespm=600;batch=64;mincontentionns=9000000;hash=0x2a;cap=0xf",
      &config).ok());
  JH_CHECK(config.profile == 3U);
  JH_CHECK(config.transport_capacity == 1024U);
  JH_CHECK(config.max_tracked_threads == 128U);
  JH_CHECK(config.max_stack_depth == 96U);
  JH_CHECK(config.min_stack_trigger_interval_ms == 50U);
  JH_CHECK(config.max_stack_samples_per_minute == 600U);
  JH_CHECK(config.config_hash == 42U);
  JH_CHECK(config.requested_capabilities == 15U);
  JH_CHECK(ParseAgentOptions("v=2", &config).code == StatusCode::kInvalidArgument);
  JH_CHECK(ParseAgentOptions("transport=18446744073709551616", &config).code ==
      StatusCode::kInvalidArgument);
  JH_CHECK(ParseAgentOptions("unknown=1", &config).code == StatusCode::kInvalidArgument);
}

class FakeCapabilityControl final : public CapabilityControl {
 public:
  CapabilitySet potential{};
  CapabilitySet granted{};
  jankhunter::artti::Status add_status{};

  jankhunter::artti::Status GetPotential(CapabilitySet* output) noexcept override {
    *output = potential;
    return jankhunter::artti::Status::Ok();
  }

  jankhunter::artti::Status Add(CapabilitySet requested) noexcept override {
    granted = requested.intersect(potential);
    return add_status;
  }

  jankhunter::artti::Status GetGranted(CapabilitySet* output) noexcept override {
    *output = granted;
    return jankhunter::artti::Status::Ok();
  }
};

void CapabilityNegotiationIsPartialAndExplicit() {
  CapabilitySet requested;
  requested.add(Capability::kGcEvents);
  requested.add(Capability::kMonitorEvents);
  FakeCapabilityControl control;
  control.potential.add(Capability::kGcEvents);
  const auto partial = NegotiateCapabilities(&control, requested);
  JH_CHECK(partial.degraded);
  JH_CHECK(partial.granted.contains(Capability::kGcEvents));
  JH_CHECK(!partial.granted.contains(Capability::kMonitorEvents));

  control.potential = requested;
  control.add_status = jankhunter::artti::Status::Ok();
  const auto complete = NegotiateCapabilities(&control, requested);
  JH_CHECK(complete.status.ok());
  JH_CHECK(!complete.degraded);
  JH_CHECK(complete.granted.bits() == requested.bits());
}

}  // namespace

int main() {
  ConfigValidation();
  BoundedIdDeduplicationAndCollisionPolicy();
  QueueWrapAndOverflow();
  QueueMultiProducerStress();
  SaturatingQualityCounters();
  GcPairing();
  MonitorPairingAndCapacity();
  ThreadRegistryLifecycle();
  EngineLifecycleAndIntervals();
  EngineStartStopRace();
  EngineQualitySnapshotIsBoundedAndCumulative();
  BatchEncodingBoundsAndEnvelope();
  AgentOptionsAreBounded();
  CapabilityNegotiationIsPartialAndExplicit();
  std::cout << "jh_artti_core_tests: PASS\n";
  return 0;
}
