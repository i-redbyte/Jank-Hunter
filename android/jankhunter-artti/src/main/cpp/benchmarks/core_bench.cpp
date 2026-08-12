#include <algorithm>
#include <array>
#include <atomic>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <iomanip>
#include <iostream>
#include <limits>
#include <string_view>
#include <thread>
#include <vector>

#include "core/bounded_mpsc_ring.h"
#include "core/bounded_id_table.h"
#include "core/engine.h"
#include "core/interval_tracker.h"
#include "core/thread_registry.h"
#include "protocol/batch_encoder.h"
#include "protocol/wire_format.h"

namespace {

using Clock = std::chrono::steady_clock;
using jankhunter::artti::BoundedMpscRing;
using jankhunter::artti::BoundedIdTable;
using jankhunter::artti::ClockSource;
using jankhunter::artti::MonitorIntervalTracker;
using jankhunter::artti::NativeConfigSnapshot;
using jankhunter::artti::NativeEngine;
using jankhunter::artti::NativeEvent;
using jankhunter::artti::QualityCounters;
using jankhunter::artti::ThreadMetadata;
using jankhunter::artti::ThreadRegistry;
using jankhunter::artti::ThreadToken;

constexpr std::uint32_t kIterations = 200'000U;

struct Distribution final {
  double p50_ns{0.0};
  double p95_ns{0.0};
  double p99_ns{0.0};
  double operations_per_second{0.0};
};

std::uint64_t Percentile(const std::vector<std::uint64_t>& sorted, double percentile) {
  const auto offset = static_cast<std::size_t>(
      std::ceil(percentile * static_cast<double>(sorted.size())) - 1.0);
  return sorted[std::min(offset, sorted.size() - 1U)];
}

template <typename Operation>
Distribution Measure(Operation operation, std::uint32_t iterations = kIterations) {
  for (std::uint32_t index = 0U; index < 10'000U; ++index) operation(index);
  std::vector<std::uint64_t> samples;
  samples.reserve(iterations);
  const auto total_start = Clock::now();
  for (std::uint32_t index = 0U; index < iterations; ++index) {
    const auto start = Clock::now();
    operation(index);
    const auto end = Clock::now();
    samples.push_back(static_cast<std::uint64_t>(
        std::chrono::duration_cast<std::chrono::nanoseconds>(end - start).count()));
  }
  const auto total_end = Clock::now();
  std::sort(samples.begin(), samples.end());
  const auto elapsed_ns = static_cast<double>(
      std::chrono::duration_cast<std::chrono::nanoseconds>(total_end - total_start).count());
  return Distribution{
      static_cast<double>(Percentile(samples, 0.50)),
      static_cast<double>(Percentile(samples, 0.95)),
      static_cast<double>(Percentile(samples, 0.99)),
      static_cast<double>(iterations) * 1'000'000'000.0 / elapsed_ns,
  };
}

void Print(std::string_view name, const Distribution& distribution, std::uint64_t drops = 0U) {
  std::cout << std::fixed << std::setprecision(1)
            << "{\"benchmark\":\"" << name << "\",\"p50_ns\":" << distribution.p50_ns
            << ",\"p95_ns\":" << distribution.p95_ns << ",\"p99_ns\":"
            << distribution.p99_ns << ",\"ops_per_second\":"
            << distribution.operations_per_second << ",\"drops\":" << drops << "}\n";
}

std::uint64_t HostNow(void*) noexcept {
  return static_cast<std::uint64_t>(
      std::chrono::duration_cast<std::chrono::nanoseconds>(Clock::now().time_since_epoch()).count());
}

void SingleProducerPublishAndDrain() {
  BoundedMpscRing<std::uint64_t> queue(1024U);
  const auto distribution = Measure([&queue](std::uint32_t index) {
    static_cast<void>(queue.TryPush(index));
    std::uint64_t output = 0U;
    static_cast<void>(queue.TryPop(&output));
  });
  Print("single_producer_publish_drain", distribution);
}

void OverflowPath() {
  BoundedMpscRing<std::uint64_t> queue(2U);
  static_cast<void>(queue.TryPush(1U));
  static_cast<void>(queue.TryPush(2U));
  std::uint64_t drops = 0U;
  const auto distribution = Measure([&queue, &drops](std::uint32_t index) {
    if (!queue.TryPush(index).ok()) ++drops;
  });
  Print("overflow_drop", distribution, drops);
}

void BatchDrain() {
  NativeConfigSnapshot config{};
  config.transport_capacity = 4096U;
  config.max_tracked_threads = 512U;
  config.max_open_contentions = 1024U;
  config.drain_batch_size = 256U;
  NativeEngine engine(config, ClockSource{nullptr, HostNow});
  static_cast<void>(engine.Start());
  std::array<NativeEvent, 256U> batch{};
  const auto distribution = Measure(
      [&engine, &batch](std::uint32_t) {
        for (std::uint32_t index = 0U; index < batch.size(); ++index) {
          static_cast<void>(engine.Publish(NativeEvent{}));
        }
        static_cast<void>(engine.Drain(batch));
      },
      2'000U);
  Print("consumer_batch_256", distribution);
}

void BatchEncode() {
  std::array<NativeEvent, 256U> batch{};
  for (std::uint32_t index = 0U; index < batch.size(); ++index) {
    batch[index].producer_sequence = index + 1U;
    batch[index].monotonic_ns = index + 100U;
  }
  std::array<std::byte,
      jankhunter::artti::protocol::kBatchHeaderSize +
          batch.size() * jankhunter::artti::protocol::kRecordSize>
      output{};
  const auto distribution = Measure(
      [&batch, &output](std::uint32_t) {
        static_cast<void>(jankhunter::artti::protocol::EncodeBatch(batch, output));
      },
      20'000U);
  Print("native_batch_encode_256", distribution);
}

void IntervalPairing() {
  MonitorIntervalTracker tracker(1024U);
  QualityCounters quality;
  NativeEvent event{};
  const auto distribution = Measure([&tracker, &quality, &event](std::uint32_t index) {
    const ThreadToken token(static_cast<std::uint64_t>(index % 512U) + 1U);
    const auto start = static_cast<std::uint64_t>(index) * 10U + 1U;
    static_cast<void>(tracker.Start(token, start, &quality));
    static_cast<void>(tracker.Finish(token, start + 5U, 1U, &event, &quality));
  });
  Print("interval_pairing", distribution);
}

void ThreadLookup() {
  ThreadRegistry registry(1024U);
  QualityCounters quality;
  NativeEvent event{};
  std::array<ThreadToken, 512U> tokens{};
  for (std::uint32_t index = 0U; index < tokens.size(); ++index) {
    static_cast<void>(registry.Register(
        ThreadMetadata{index + 1U, 0U, 0U, 0U, false}, index + 1U, &tokens[index], &event, &quality));
  }
  ThreadMetadata metadata{};
  const auto distribution = Measure([&registry, &tokens, &metadata](std::uint32_t index) {
    static_cast<void>(registry.Lookup(tokens[index % tokens.size()], &metadata));
  });
  Print("thread_lookup", distribution);
}

void StackFingerprintDeduplication() {
  BoundedIdTable table;
  if (!table.Initialize(4096U)) std::abort();
  for (std::uint64_t id = 1U; id <= 2048U; ++id) static_cast<void>(table.Insert(id));
  const auto distribution = Measure([&table](const std::uint32_t index) {
    const auto fingerprint = static_cast<std::uint64_t>(index % 2048U) + 1U;
    static_cast<void>(table.Insert(fingerprint));
  });
  Print("stack_fingerprint_dedup", distribution);
}

void MultiProducerContention() {
  constexpr std::uint32_t kThreads = 8U;
  constexpr std::uint32_t kOperationsPerThread = 100'000U;
  BoundedMpscRing<std::uint64_t> queue(4096U);
  std::atomic<bool> start{false};
  std::atomic<std::uint32_t> done{0U};
  std::atomic<std::uint64_t> drops{0U};
  std::vector<std::thread> producers;
  producers.reserve(kThreads);
  for (std::uint32_t thread_index = 0U; thread_index < kThreads; ++thread_index) {
    producers.emplace_back([thread_index, &queue, &start, &done, &drops] {
      while (!start.load(std::memory_order_acquire)) std::this_thread::yield();
      for (std::uint32_t index = 0U; index < kOperationsPerThread; ++index) {
        const auto value = (static_cast<std::uint64_t>(thread_index) << 32U) | index;
        if (!queue.TryPush(value).ok()) drops.fetch_add(1U, std::memory_order_relaxed);
      }
      done.fetch_add(1U, std::memory_order_release);
    });
  }
  const auto begin = Clock::now();
  start.store(true, std::memory_order_release);
  std::uint64_t drained = 0U;
  while (done.load(std::memory_order_acquire) != kThreads || queue.ApproximateSize() != 0U) {
    std::uint64_t output = 0U;
    if (queue.TryPop(&output).ok()) ++drained;
  }
  const auto end = Clock::now();
  for (auto& producer : producers) producer.join();
  const auto total = static_cast<double>(kThreads) * kOperationsPerThread;
  const auto elapsed_ns = static_cast<double>(
      std::chrono::duration_cast<std::chrono::nanoseconds>(end - begin).count());
  const Distribution distribution{0.0, 0.0, 0.0, total * 1'000'000'000.0 / elapsed_ns};
  Print("multi_producer_contention", distribution, drops.load(std::memory_order_relaxed));
  if (drained + drops.load(std::memory_order_relaxed) != static_cast<std::uint64_t>(total)) {
    std::abort();
  }
}

void StartStopCycles() {
  const auto distribution = Measure(
      [](std::uint32_t) {
        NativeConfigSnapshot config{};
        config.transport_capacity = 64U;
        config.max_tracked_threads = 16U;
        config.max_open_contentions = 16U;
        config.drain_batch_size = 16U;
        NativeEngine engine(config, ClockSource{nullptr, HostNow});
        static_cast<void>(engine.Start());
        static_cast<void>(engine.BeginStop());
        engine.MarkStopped();
      },
      10'000U);
  Print("start_stop_cycle", distribution);
}

}  // namespace

int main() {
  SingleProducerPublishAndDrain();
  MultiProducerContention();
  OverflowPath();
  BatchDrain();
  BatchEncode();
  IntervalPairing();
  ThreadLookup();
  StackFingerprintDeduplication();
  StartStopCycles();
  return 0;
}
