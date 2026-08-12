#include "core/thread_registry.h"

#include <algorithm>
#include <new>

namespace jankhunter::artti {

ThreadRegistry::ThreadRegistry(const std::uint32_t capacity) noexcept
    : capacity_(capacity),
      entries_(capacity == 0U ? nullptr : std::unique_ptr<Entry[]>(new (std::nothrow) Entry[capacity])) {}

Status ThreadRegistry::Register(
    const ThreadMetadata& metadata,
    const std::uint64_t timestamp_ns,
    ThreadToken* const token,
    NativeEvent* const event,
    QualityCounters* const quality) noexcept {
  if (!valid() || token == nullptr || event == nullptr || timestamp_ns == 0U) {
    if (quality != nullptr) quality->Add(QualityCounter::kInvalidInput);
    return Status::Error(StatusCode::kInvalidArgument);
  }
  const auto raw_token = next_token_.fetch_add(1U, std::memory_order_relaxed);
  if (raw_token == kEmpty || raw_token >= kReserved) {
    if (quality != nullptr) quality->Add(QualityCounter::kThreadCapacityLoss);
    return Status::Error(StatusCode::kCapacityExhausted);
  }
  const ThreadToken assigned(raw_token);
  const auto probe_count = std::min(capacity_, kMaxProbe);
  const auto initial = InitialIndex(assigned);
  for (std::uint32_t probe = 0U; probe < probe_count; ++probe) {
    Entry& entry = entries_[(initial + probe) % capacity_];
    auto observed = entry.token.load(std::memory_order_acquire);
    if (observed != kEmpty && observed != kTombstone) continue;
    if (!entry.token.compare_exchange_strong(
            observed, kReserved, std::memory_order_acq_rel, std::memory_order_acquire)) {
      continue;
    }
    StoreMetadata(&entry, metadata);
    entry.token.store(raw_token, std::memory_order_release);
    *token = assigned;
    FillThreadEvent(event, EventType::kThreadStart, assigned, timestamp_ns, metadata);
    return Status::Ok();
  }
  if (quality != nullptr) quality->Add(QualityCounter::kThreadCapacityLoss);
  return Status::Error(StatusCode::kCapacityExhausted);
}

Status ThreadRegistry::Update(const ThreadToken token, const ThreadMetadata& metadata) noexcept {
  Entry* const entry = Find(token);
  if (entry == nullptr) return Status::Error(StatusCode::kNotFound);
  auto expected = token.value();
  if (!entry->token.compare_exchange_strong(
          expected, kReserved, std::memory_order_acq_rel, std::memory_order_acquire)) {
    return Status::Error(StatusCode::kContended);
  }
  StoreMetadata(entry, metadata);
  entry->token.store(token.value(), std::memory_order_release);
  return Status::Ok();
}

Status ThreadRegistry::Lookup(const ThreadToken token, ThreadMetadata* const metadata) const noexcept {
  if (metadata == nullptr) return Status::Error(StatusCode::kInvalidArgument);
  const Entry* const entry = Find(token);
  if (entry == nullptr) return Status::Error(StatusCode::kNotFound);
  *metadata = LoadMetadata(*entry);
  if (entry->token.load(std::memory_order_acquire) != token.value()) {
    return Status::Error(StatusCode::kContended);
  }
  return Status::Ok();
}

Status ThreadRegistry::Release(
    const ThreadToken token,
    const std::uint64_t timestamp_ns,
    NativeEvent* const event,
    QualityCounters* const quality) noexcept {
  if (!token.valid() || event == nullptr || timestamp_ns == 0U) {
    if (quality != nullptr) quality->Add(QualityCounter::kInvalidInput);
    return Status::Error(StatusCode::kInvalidArgument);
  }
  Entry* const entry = Find(token);
  if (entry == nullptr) {
    if (quality != nullptr) quality->Add(QualityCounter::kThreadUnknownEnd);
    return Status::Error(StatusCode::kNotFound);
  }
  auto expected = token.value();
  if (!entry->token.compare_exchange_strong(
          expected, kReserved, std::memory_order_acq_rel, std::memory_order_acquire)) {
    if (quality != nullptr) quality->Add(QualityCounter::kThreadUnknownEnd);
    return Status::Error(StatusCode::kContended);
  }
  const auto metadata = LoadMetadata(*entry);
  FillThreadEvent(event, EventType::kThreadEnd, token, timestamp_ns, metadata);
  StoreMetadata(entry, ThreadMetadata{});
  entry->token.store(kTombstone, std::memory_order_release);
  return Status::Ok();
}

std::uint64_t ThreadRegistry::Reset() noexcept {
  if (!valid()) return 0U;
  std::uint64_t active = 0U;
  for (std::uint32_t index = 0U; index < capacity_; ++index) {
    const auto token = entries_[index].token.exchange(kEmpty, std::memory_order_acq_rel);
    if (token != kEmpty && token != kTombstone && token != kReserved) ++active;
    StoreMetadata(&entries_[index], ThreadMetadata{});
  }
  return active;
}

std::size_t ThreadRegistry::MemoryBytes() const noexcept {
  return static_cast<std::size_t>(capacity_) * sizeof(Entry);
}

std::uint32_t ThreadRegistry::InitialIndex(const ThreadToken token) const noexcept {
  const auto value = token.value();
  const auto mixed = value ^ (value >> 29U) ^ (value << 17U);
  return static_cast<std::uint32_t>(mixed % capacity_);
}

ThreadRegistry::Entry* ThreadRegistry::Find(const ThreadToken token) noexcept {
  return const_cast<Entry*>(static_cast<const ThreadRegistry*>(this)->Find(token));
}

const ThreadRegistry::Entry* ThreadRegistry::Find(const ThreadToken token) const noexcept {
  if (!valid() || !token.valid()) return nullptr;
  const auto probe_count = std::min(capacity_, kMaxProbe);
  const auto initial = InitialIndex(token);
  for (std::uint32_t probe = 0U; probe < probe_count; ++probe) {
    const Entry& entry = entries_[(initial + probe) % capacity_];
    const auto observed = entry.token.load(std::memory_order_acquire);
    if (observed == token.value()) return &entry;
    if (observed == kEmpty) return nullptr;
  }
  return nullptr;
}

void ThreadRegistry::StoreMetadata(Entry* const entry, const ThreadMetadata& metadata) noexcept {
  entry->linux_tid.store(metadata.linux_tid, std::memory_order_relaxed);
  entry->name_id.store(metadata.name_id, std::memory_order_relaxed);
  entry->state.store(metadata.state, std::memory_order_relaxed);
  entry->category.store(metadata.category, std::memory_order_relaxed);
  entry->daemon.store(metadata.daemon ? 1U : 0U, std::memory_order_relaxed);
}

ThreadMetadata ThreadRegistry::LoadMetadata(const Entry& entry) noexcept {
  return ThreadMetadata{
      entry.linux_tid.load(std::memory_order_relaxed),
      entry.name_id.load(std::memory_order_relaxed),
      entry.state.load(std::memory_order_relaxed),
      entry.category.load(std::memory_order_relaxed),
      entry.daemon.load(std::memory_order_relaxed) != 0U,
  };
}

void ThreadRegistry::FillThreadEvent(
    NativeEvent* const event,
    const EventType type,
    const ThreadToken token,
    const std::uint64_t timestamp_ns,
    const ThreadMetadata& metadata) noexcept {
  *event = NativeEvent{};
  event->type = type;
  event->monotonic_ns = timestamp_ns;
  event->thread_token = token.value();
  event->payload.thread.linux_tid = metadata.linux_tid;
  event->payload.thread.name_id = metadata.name_id;
  event->payload.thread.state = metadata.state;
  event->payload.thread.category = metadata.category;
  event->payload.thread.daemon = metadata.daemon ? 1U : 0U;
}

}  // namespace jankhunter::artti
