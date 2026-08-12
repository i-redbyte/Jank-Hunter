#ifndef JANKHUNTER_ARTTI_CORE_EVENT_H_
#define JANKHUNTER_ARTTI_CORE_EVENT_H_

#include <cstddef>
#include <cstdint>
#include <type_traits>

#include "core/ids.h"

namespace jankhunter::artti {

enum class EventType : std::uint16_t {
  kAgentStatus = 1,
  kCapability = 2,
  kQualitySnapshot = 3,
  kThreadStart = 4,
  kThreadEnd = 5,
  kGcInterval = 6,
  kMonitorContentionInterval = 7,
  kThreadStackSample = 8,
  kStackDefinition = 9,
  kClockSync = 10,
  kCorrelationLink = 11,
};

enum class StackTrigger : std::uint32_t {
  kUnknown = 0,
  kMainThreadStall = 1,
  kLongContention = 2,
  kDeepManual = 3,
};

struct IntervalPayload final {
  std::uint64_t start_ns{0U};
  std::uint64_t duration_ns{0U};
  std::uint64_t related_token{0U};
  std::uint32_t reason{0U};
  std::uint32_t reserved{0U};
};

struct ThreadPayload final {
  std::uint64_t linux_tid{0U};
  std::uint64_t name_id{0U};
  std::uint32_t state{0U};
  std::uint32_t category{0U};
  std::uint32_t daemon{0U};
  std::uint32_t reserved{0U};
};

struct StackPayload final {
  std::uint64_t fingerprint{0U};
  std::uint64_t related_sequence{0U};
  std::uint32_t frame_count{0U};
  std::uint32_t trigger{0U};
  std::uint32_t truncated{0U};
  std::uint32_t reserved{0U};
};

struct StatusPayload final {
  std::uint64_t value0{0U};
  std::uint64_t value1{0U};
  std::uint64_t value2{0U};
  std::uint64_t value3{0U};
};

union EventPayload final {
  constexpr EventPayload() noexcept : status{} {}
  IntervalPayload interval;
  ThreadPayload thread;
  StackPayload stack;
  StatusPayload status;
};

struct NativeEvent final {
  std::uint64_t producer_sequence{0U};
  std::uint64_t monotonic_ns{0U};
  std::uint64_t producer_id{0U};
  std::uint64_t thread_token{0U};
  std::uint64_t context_token{0U};
  EventType type{EventType::kAgentStatus};
  std::uint16_t schema_version{1U};
  std::uint32_t flags{0U};
  EventPayload payload{};
};

static_assert(std::is_trivially_copyable_v<NativeEvent>);
static_assert(sizeof(NativeEvent) <= 128U);
static_assert(alignof(NativeEvent) >= alignof(std::uint64_t));

}  // namespace jankhunter::artti

#endif  // JANKHUNTER_ARTTI_CORE_EVENT_H_
