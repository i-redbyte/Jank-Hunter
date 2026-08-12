#include "art/art_jvmti_adapter.h"

#include <algorithm>
#include <chrono>

#include "art/jvmti_resources.h"
#include "bridge/bridge_runtime.h"

namespace jankhunter::artti::art {
namespace {

constexpr std::uint64_t kMinuteNs = 60'000'000'000ULL;

void HashU64(const std::uint64_t value, std::uint64_t *const hash) noexcept {
  for (std::uint32_t index = 0U; index < 8U; ++index) {
    *hash ^= (value >> (index * 8U)) & 0xFFU;
    *hash *= 1099511628211ULL;
  }
}

} // namespace

std::int32_t
ArtJvmtiAdapter::CaptureStack(JNIEnv *, jthread thread,
                              const std::uint32_t trigger,
                              const std::uint64_t context_token,
                              const std::uint64_t related_sequence) noexcept {
  std::lock_guard lock(control_mutex_);
  return CaptureStackLocked(thread, trigger, context_token, related_sequence);
}

std::int32_t ArtJvmtiAdapter::CaptureStackForToken(
    JNIEnv *const jni, const std::uint64_t thread_token,
    const std::uint32_t trigger, const std::uint64_t context_token,
    const std::uint64_t related_sequence) noexcept {
  std::lock_guard lock(control_mutex_);
  if (!attached_ || jvmti_ == nullptr || jni == nullptr || thread_token == 0U) {
    return -static_cast<std::int32_t>(StatusCode::kInvalidState);
  }

  jint count = 0;
  JvmtiAllocation<jthread> threads(jvmti_);
  const auto error = jvmti_->GetAllThreads(&count, threads.out());
  if (error != JVMTI_ERROR_NONE || count < 0 ||
      (count > 0 && threads.get() == nullptr)) {
    RecordJvmtiError(error);
    return -static_cast<std::int32_t>(StatusCode::kUnsupported);
  }

  std::int32_t result = -static_cast<std::int32_t>(StatusCode::kNotFound);
  const auto limit = std::min<std::uint32_t>(static_cast<std::uint32_t>(count),
                                             config_.max_tracked_threads);
  for (std::uint32_t index = 0U; index < static_cast<std::uint32_t>(count);
       ++index) {
    jthread thread = threads[index];
    JniLocalRef<jthread> thread_ref(jni, thread);
    if (index < limit && result < 0 &&
        TokenFor(thread).value() == thread_token) {
      result =
          CaptureStackLocked(thread, trigger, context_token, related_sequence);
    }
  }
  return result;
}

std::int32_t
ArtJvmtiAdapter::LinkThreadContext(jthread thread,
                                   const std::uint64_t context_token) noexcept {
  if (thread == nullptr || context_token == 0U) {
    return -static_cast<std::int32_t>(StatusCode::kInvalidArgument);
  }
  CallbackScope operation(this);
  NativeEngine *const engine = operation.engine();
  if (engine == nullptr)
    return -static_cast<std::int32_t>(StatusCode::kClosed);
  const auto token = EnsureCallbackThreadToken(engine, thread);
  if (!token.valid())
    return -static_cast<std::int32_t>(StatusCode::kNotFound);

  NativeEvent event{};
  event.type = EventType::kCorrelationLink;
  event.thread_token = token.value();
  event.context_token = context_token;
  event.payload.status.value0 = context_token;
  return engine->Publish(event).ok()
             ? static_cast<std::int32_t>(StatusCode::kOk)
             : -static_cast<std::int32_t>(StatusCode::kQueueFull);
}

std::int32_t ArtJvmtiAdapter::CaptureStackLocked(
    jthread thread, const std::uint32_t trigger,
    const std::uint64_t context_token,
    const std::uint64_t related_sequence) noexcept {
  NativeEngine *const engine =
      bridge::BridgeRuntime::Instance().callback_engine();
  if (!attached_ || jvmti_ == nullptr || engine == nullptr ||
      thread == nullptr ||
      !active_capabilities().contains(Capability::kStackTrace) ||
      trigger == 0U || trigger > 3U) {
    return -static_cast<std::int32_t>(StatusCode::kInvalidState);
  }

  const auto max_per_minute = config_.max_stack_samples_per_minute;
  const auto min_interval_ns =
      static_cast<std::uint64_t>(config_.min_stack_trigger_interval_ms) *
      1'000'000U;
  const auto now_ns = static_cast<std::uint64_t>(
      std::chrono::duration_cast<std::chrono::nanoseconds>(
          std::chrono::steady_clock::now().time_since_epoch())
          .count());
  if (stack_budget_window_start_ns_ == 0U ||
      now_ns - stack_budget_window_start_ns_ >= kMinuteNs) {
    stack_budget_window_start_ns_ = now_ns;
    stack_captures_in_window_ = 0U;
  }
  const bool too_soon = last_stack_capture_ns_ != 0U &&
                        now_ns - last_stack_capture_ns_ < min_interval_ns;
  if (stack_captures_in_window_ >= max_per_minute || too_soon) {
    engine->quality().Add(QualityCounter::kStackCaptureBudgetLoss);
    return -static_cast<std::int32_t>(StatusCode::kContended);
  }
  last_stack_capture_ns_ = now_ns;
  ++stack_captures_in_window_;

  std::array<jvmtiFrameInfo, 256U> frames{};
  jint frame_count = 0;
  const auto depth = static_cast<jint>(
      std::min<std::uint32_t>(config_.max_stack_depth, frames.size()));
  const auto error =
      jvmti_->GetStackTrace(thread, 0, depth, frames.data(), &frame_count);
  if (error != JVMTI_ERROR_NONE || frame_count < 0 || frame_count > depth) {
    engine->quality().Add(QualityCounter::kStackCaptureFailure);
    RecordJvmtiError(error);
    if (error == JVMTI_ERROR_NOT_AVAILABLE ||
        error == JVMTI_ERROR_MUST_POSSESS_CAPABILITY) {
      RemoveActiveCapability(Capability::kStackTrace);
      PublishCapabilities();
    }
    return -static_cast<std::int32_t>(StatusCode::kUnsupported);
  }

  std::uint64_t fingerprint = 1469598103934665603ULL;
  for (jint index = 0; index < frame_count; ++index) {
    HashU64(static_cast<std::uint64_t>(reinterpret_cast<std::uintptr_t>(
                frames[static_cast<std::size_t>(index)].method)),
            &fingerprint);
    HashU64(static_cast<std::uint64_t>(
                frames[static_cast<std::size_t>(index)].location),
            &fingerprint);
  }
  if (fingerprint == 0U)
    fingerprint = 1U;

  const auto insert = stack_fingerprints_.Insert(fingerprint);
  if (insert == BoundedIdInsertResult::kFull) {
    engine->quality().Add(QualityCounter::kStackDefinitionCapacityLoss);
  } else if (insert == BoundedIdInsertResult::kInserted) {
    PublishStackDefinition(engine, frames, frame_count, fingerprint,
                           context_token);
  }

  NativeEvent sample{};
  sample.type = EventType::kThreadStackSample;
  sample.context_token = context_token;
  sample.thread_token = TokenFor(thread).value();
  sample.payload.stack.fingerprint = fingerprint;
  sample.payload.stack.related_sequence = related_sequence;
  sample.payload.stack.frame_count = static_cast<std::uint32_t>(frame_count);
  sample.payload.stack.trigger = trigger;
  sample.payload.stack.truncated = frame_count == depth ? 1U : 0U;
  return engine->Publish(sample).ok()
             ? frame_count
             : -static_cast<std::int32_t>(StatusCode::kQueueFull);
}

void ArtJvmtiAdapter::PublishStackDefinition(
    NativeEngine *const engine, const std::span<const jvmtiFrameInfo> frames,
    const jint frame_count, const std::uint64_t fingerprint,
    const std::uint64_t context_token) noexcept {
  for (jint index = 0; index < frame_count; ++index) {
    NativeEvent definition{};
    definition.type = EventType::kStackDefinition;
    definition.context_token = context_token;
    definition.payload.status.value0 = fingerprint;
    definition.payload.status.value1 =
        static_cast<std::uint64_t>(reinterpret_cast<std::uintptr_t>(
            frames[static_cast<std::size_t>(index)].method));
    definition.payload.status.value2 = static_cast<std::uint64_t>(
        frames[static_cast<std::size_t>(index)].location);
    definition.payload.status.value3 =
        static_cast<std::uint64_t>(static_cast<std::uint32_t>(index)) |
        (static_cast<std::uint64_t>(static_cast<std::uint32_t>(frame_count))
         << 32U);
    if (!engine->Publish(definition).ok()) {
      engine->quality().Add(QualityCounter::kStackDefinitionPublishLoss);
      return;
    }
  }
}

} // namespace jankhunter::artti::art
