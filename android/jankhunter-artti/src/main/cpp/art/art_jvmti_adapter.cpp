#include "art/art_jvmti_adapter.h"

#include <sys/syscall.h>
#include <unistd.h>

#include <algorithm>
#include <chrono>
#include <cstring>
#include <limits>
#include <thread>

#include "art/agent_options.h"
#include "art/capability_negotiator.h"
#include "bridge/bridge_runtime.h"
#include "protocol/method_wire.h"

namespace jankhunter::artti::art {
namespace {

constexpr std::uint64_t kStatusAttachStarted = 1U;
constexpr std::uint64_t kStatusAttachSucceeded = 2U;
constexpr std::uint64_t kStatusAttachDegraded = 3U;
constexpr std::uint64_t kStatusAttachFailed = 4U;
constexpr std::uint64_t kStatusStopped = 5U;
constexpr std::uint64_t kMinuteNs = 60'000'000'000ULL;

std::uint64_t CurrentTid() noexcept {
  const auto tid = syscall(SYS_gettid);
  return tid <= 0 ? 0U : static_cast<std::uint64_t>(tid);
}

std::uint64_t HashBytes(const char* value) noexcept {
  if (value == nullptr) return 0U;
  std::uint64_t hash = 1469598103934665603ULL;
  for (const auto* cursor = reinterpret_cast<const unsigned char*>(value); *cursor != 0U; ++cursor) {
    hash ^= *cursor;
    hash *= 1099511628211ULL;
  }
  return hash == 0U ? 1U : hash;
}

void HashU64(std::uint64_t value, std::uint64_t* hash) noexcept {
  for (std::uint32_t index = 0U; index < 8U; ++index) {
    *hash ^= (value >> (index * 8U)) & 0xFFU;
    *hash *= 1099511628211ULL;
  }
}

class JvmtiCapabilityControl final : public CapabilityControl {
 public:
  explicit JvmtiCapabilityControl(jvmtiEnv* env) noexcept : env_(env) {}

  Status GetPotential(CapabilitySet* output) noexcept override {
    if (env_ == nullptr || output == nullptr) return Status::Error(StatusCode::kInvalidArgument);
    jvmtiCapabilities capabilities{};
    const auto error = env_->GetPotentialCapabilities(&capabilities);
    if (error != JVMTI_ERROR_NONE) return Status::Error(StatusCode::kUnsupported);
    CapabilitySet result;
    if (capabilities.can_generate_garbage_collection_events != 0U) result.add(Capability::kGcEvents);
    if (capabilities.can_generate_monitor_events != 0U) result.add(Capability::kMonitorEvents);
    *output = result;
    return Status::Ok();
  }

  Status Add(const CapabilitySet requested) noexcept override {
    if (env_ == nullptr) return Status::Error(StatusCode::kInvalidArgument);
    jvmtiCapabilities capabilities{};
    capabilities.can_generate_garbage_collection_events =
        requested.contains(Capability::kGcEvents) ? 1U : 0U;
    capabilities.can_generate_monitor_events = requested.contains(Capability::kMonitorEvents) ? 1U : 0U;
    const auto error = env_->AddCapabilities(&capabilities);
    return error == JVMTI_ERROR_NONE ? Status::Ok() : Status::Error(StatusCode::kUnsupported);
  }

  Status GetGranted(CapabilitySet* output) noexcept override {
    if (env_ == nullptr || output == nullptr) return Status::Error(StatusCode::kInvalidArgument);
    jvmtiCapabilities capabilities{};
    const auto error = env_->GetCapabilities(&capabilities);
    if (error != JVMTI_ERROR_NONE) return Status::Error(StatusCode::kUnsupported);
    CapabilitySet result;
    if (capabilities.can_generate_garbage_collection_events != 0U) result.add(Capability::kGcEvents);
    if (capabilities.can_generate_monitor_events != 0U) result.add(Capability::kMonitorEvents);
    *output = result;
    return Status::Ok();
  }

 private:
  jvmtiEnv* env_;
};

class JvmtiString final {
 public:
  JvmtiString(jvmtiEnv* env, char* value) noexcept : env_(env), value_(value) {}
  ~JvmtiString() {
    if (env_ != nullptr && value_ != nullptr) static_cast<void>(env_->Deallocate(
        reinterpret_cast<unsigned char*>(value_)));
  }
  JvmtiString(const JvmtiString&) = delete;
  JvmtiString& operator=(const JvmtiString&) = delete;
  [[nodiscard]] const char* get() const noexcept { return value_; }

 private:
  jvmtiEnv* env_;
  char* value_;
};

void WriteU16(std::span<std::byte> output, const std::size_t offset, const std::uint16_t value) noexcept {
  output[offset] = static_cast<std::byte>(value & 0xFFU);
  output[offset + 1U] = static_cast<std::byte>((value >> 8U) & 0xFFU);
}

void WriteU32(std::span<std::byte> output, const std::size_t offset, const std::uint32_t value) noexcept {
  for (std::size_t index = 0U; index < sizeof(value); ++index) {
    output[offset + index] = static_cast<std::byte>((value >> (index * 8U)) & 0xFFU);
  }
}

void WriteU64(std::span<std::byte> output, const std::size_t offset, const std::uint64_t value) noexcept {
  for (std::size_t index = 0U; index < sizeof(value); ++index) {
    output[offset + index] = static_cast<std::byte>((value >> (index * 8U)) & 0xFFU);
  }
}

}  // namespace

ArtJvmtiAdapter& ArtJvmtiAdapter::Instance() noexcept {
  static ArtJvmtiAdapter adapter;
  return adapter;
}

ArtJvmtiAdapter::CallbackScope::CallbackScope(ArtJvmtiAdapter* const adapter) noexcept
    : adapter_(adapter) {
  if (adapter_ == nullptr) return;
  if (!adapter_->accepting_callbacks_.load(std::memory_order_acquire)) {
    NativeEngine* const engine = bridge::BridgeRuntime::Instance().callback_engine();
    if (engine != nullptr) engine->quality().Add(QualityCounter::kCallbackAfterStop);
    adapter_ = nullptr;
    return;
  }
  adapter_->active_callbacks_.fetch_add(1U, std::memory_order_acq_rel);
  if (!adapter_->accepting_callbacks_.load(std::memory_order_acquire)) {
    NativeEngine* const engine = bridge::BridgeRuntime::Instance().callback_engine();
    if (engine != nullptr) engine->quality().Add(QualityCounter::kCallbackAfterStop);
    adapter_->active_callbacks_.fetch_sub(1U, std::memory_order_acq_rel);
    adapter_ = nullptr;
    return;
  }
  engine_ = bridge::BridgeRuntime::Instance().callback_engine();
  if (engine_ == nullptr) {
    adapter_->active_callbacks_.fetch_sub(1U, std::memory_order_acq_rel);
    adapter_ = nullptr;
  }
}

ArtJvmtiAdapter::CallbackScope::~CallbackScope() {
  if (adapter_ != nullptr) adapter_->active_callbacks_.fetch_sub(1U, std::memory_order_acq_rel);
}

Status ArtJvmtiAdapter::Attach(
    JavaVM* const vm,
    jvmtiEnv* const jvmti,
    const bridge::ArtTiNativeConfigV1& wire_config) noexcept {
  if (vm == nullptr || jvmti == nullptr) return Status::Error(StatusCode::kInvalidArgument);
  std::lock_guard lock(control_mutex_);
  if (attached_) {
    return config_.config_hash == wire_config.config_hash
        ? Status::Ok()
        : Status::Error(StatusCode::kInvalidState);
  }
  vm_ = vm;
  jvmti_ = jvmti;
  config_ = bridge::BridgeRuntime::ConfigFromWire(wire_config);
  requested_ = config_.requested_capabilities;
  PublishStatus(kStatusAttachStarted, 0U);
  if (!stack_fingerprints_.Initialize(config_.max_stack_definitions) ||
      !method_ids_.Initialize(config_.max_method_definitions)) {
    stack_fingerprints_.Reset();
    method_ids_.Reset();
    PublishStatus(kStatusAttachFailed, static_cast<std::uint64_t>(StatusCode::kInternal));
    return Status::Error(StatusCode::kInternal);
  }
  const auto capability_status = ConfigureCapabilities();
  if (!ConfigureCallbacks().ok()) {
    PublishStatus(kStatusAttachFailed, static_cast<std::uint64_t>(last_error_.load()));
    stack_fingerprints_.Reset();
    method_ids_.Reset();
    return Status::Error(StatusCode::kUnsupported);
  }
  accepting_callbacks_.store(true, std::memory_order_release);

  CapabilitySet active;
  if (requested_.contains(Capability::kThreadEvents) &&
      EnablePair(JVMTI_EVENT_THREAD_START, JVMTI_EVENT_THREAD_END)) {
    active.add(Capability::kThreadEvents);
    active.add(Capability::kLinuxTid);
  }
  if (granted_.contains(Capability::kGcEvents) &&
      EnablePair(JVMTI_EVENT_GARBAGE_COLLECTION_START, JVMTI_EVENT_GARBAGE_COLLECTION_FINISH)) {
    active.add(Capability::kGcEvents);
  }
  if (granted_.contains(Capability::kMonitorEvents) &&
      EnablePair(JVMTI_EVENT_MONITOR_CONTENDED_ENTER, JVMTI_EVENT_MONITOR_CONTENDED_ENTERED)) {
    active.add(Capability::kMonitorEvents);
  }
  if (requested_.contains(Capability::kStackTrace)) active.add(Capability::kStackTrace);
  active_bits_.store(active.bits(), std::memory_order_release);

  JNIEnv* jni = nullptr;
  if (active.contains(Capability::kThreadEvents) &&
      vm_->GetEnv(reinterpret_cast<void**>(&jni), JNI_VERSION_1_6) == JNI_OK && jni != nullptr) {
    if (SnapshotThreads(jni) > 0) {
      active.add(Capability::kThreadMetadata);
      active_bits_.store(active.bits(), std::memory_order_release);
    }
  }
  attached_ = true;
  PublishCapabilities();
  const bool degraded = !capability_status.ok() ||
      active.intersect(requested_).bits() != requested_.bits();
  PublishStatus(degraded ? kStatusAttachDegraded : kStatusAttachSucceeded, 0U);
  return Status::Ok();
}

Status ArtJvmtiAdapter::Stop(const std::uint32_t timeout_ms) noexcept {
  std::lock_guard lock(control_mutex_);
  if (!attached_) return Status::Ok();
  accepting_callbacks_.store(false, std::memory_order_release);
  DisableConfiguredEvents();
  const auto deadline = std::chrono::steady_clock::now() + std::chrono::milliseconds(timeout_ms);
  while (active_callbacks_.load(std::memory_order_acquire) != 0U &&
         std::chrono::steady_clock::now() < deadline) {
    std::this_thread::yield();
  }
  const bool clean = active_callbacks_.load(std::memory_order_acquire) == 0U;
  PublishStatus(kStatusStopped, clean ? 0U : 1U);
  if (!clean) return Status::Error(StatusCode::kContended);
  active_bits_.store(0U, std::memory_order_release);
  stack_fingerprints_.Reset();
  method_ids_.Reset();
  stack_budget_window_start_ns_ = 0U;
  last_stack_capture_ns_ = 0U;
  stack_captures_in_window_ = 0U;
  attached_ = false;
  return Status::Ok();
}

Status ArtJvmtiAdapter::ConfigureCapabilities() noexcept {
  CapabilitySet gated_requested;
  if (requested_.contains(Capability::kGcEvents)) gated_requested.add(Capability::kGcEvents);
  if (requested_.contains(Capability::kMonitorEvents)) gated_requested.add(Capability::kMonitorEvents);
  JvmtiCapabilityControl control(jvmti_);
  const auto negotiation = NegotiateCapabilities(&control, gated_requested);
  potential_ = negotiation.potential;
  granted_ = negotiation.granted;
  if (requested_.contains(Capability::kThreadEvents)) {
    potential_.add(Capability::kThreadEvents);
    granted_.add(Capability::kThreadEvents);
  }
  if (requested_.contains(Capability::kStackTrace)) {
    potential_.add(Capability::kStackTrace);
    granted_.add(Capability::kStackTrace);
  }
  if (requested_.contains(Capability::kThreadMetadata)) {
    potential_.add(Capability::kThreadMetadata);
    granted_.add(Capability::kThreadMetadata);
  }
  if (requested_.contains(Capability::kLinuxTid)) {
    potential_.add(Capability::kLinuxTid);
    granted_.add(Capability::kLinuxTid);
  }
  return negotiation.status;
}

Status ArtJvmtiAdapter::ConfigureCallbacks() noexcept {
  jvmtiEventCallbacks callbacks{};
  callbacks.ThreadStart = &ThreadStartCallback;
  callbacks.ThreadEnd = &ThreadEndCallback;
  callbacks.GarbageCollectionStart = &GcStartCallback;
  callbacks.GarbageCollectionFinish = &GcFinishCallback;
  callbacks.MonitorContendedEnter = &MonitorEnterCallback;
  callbacks.MonitorContendedEntered = &MonitorEnteredCallback;
  const auto error = jvmti_->SetEventCallbacks(&callbacks, sizeof(callbacks));
  if (error != JVMTI_ERROR_NONE) {
    RecordJvmtiError(error);
    return Status::Error(StatusCode::kUnsupported);
  }
  return Status::Ok();
}

bool ArtJvmtiAdapter::EnablePair(const jvmtiEvent first, const jvmtiEvent second) noexcept {
  if (!EnableSingle(first)) return false;
  if (EnableSingle(second)) return true;
  static_cast<void>(jvmti_->SetEventNotificationMode(JVMTI_DISABLE, first, nullptr));
  if (enabled_event_count_ > 0U) --enabled_event_count_;
  return false;
}

bool ArtJvmtiAdapter::EnableSingle(const jvmtiEvent event) noexcept {
  if (enabled_event_count_ >= enabled_events_.size()) return false;
  const auto error = jvmti_->SetEventNotificationMode(JVMTI_ENABLE, event, nullptr);
  if (error != JVMTI_ERROR_NONE) {
    RecordJvmtiError(error);
    return false;
  }
  enabled_events_[enabled_event_count_++] = event;
  return true;
}

void ArtJvmtiAdapter::DisableConfiguredEvents() noexcept {
  if (jvmti_ == nullptr) return;
  for (std::uint32_t index = 0U; index < enabled_event_count_; ++index) {
    const auto error = jvmti_->SetEventNotificationMode(JVMTI_DISABLE, enabled_events_[index], nullptr);
    if (error != JVMTI_ERROR_NONE) RecordJvmtiError(error);
  }
  enabled_event_count_ = 0U;
}

std::int32_t ArtJvmtiAdapter::SnapshotThreads(JNIEnv* const jni) noexcept {
  if (jni == nullptr || jvmti_ == nullptr) return -static_cast<std::int32_t>(StatusCode::kInvalidState);
  jint count = 0;
  jthread* threads = nullptr;
  const auto error = jvmti_->GetAllThreads(&count, &threads);
  if (error != JVMTI_ERROR_NONE || count < 0 || (count > 0 && threads == nullptr)) {
    RecordJvmtiError(error);
    if (threads != nullptr) static_cast<void>(jvmti_->Deallocate(
        reinterpret_cast<unsigned char*>(threads)));
    return -static_cast<std::int32_t>(StatusCode::kUnsupported);
  }
  NativeEngine* const engine = bridge::BridgeRuntime::Instance().callback_engine();
  std::int32_t updated = 0;
  const auto limit = std::min<std::uint32_t>(
      static_cast<std::uint32_t>(count), config_.max_tracked_threads);
  for (std::uint32_t index = 0U; index < limit; ++index) {
    jthread thread = threads[index];
    bool valid = false;
    const auto metadata = ReadThreadMetadata(jni, thread, &valid);
    if (valid && engine != nullptr) {
      auto token = TokenFor(thread);
      if (!token.valid()) {
        if (engine->OnThreadStart(metadata, &token).ok()) {
          const auto tls_error = jvmti_->SetThreadLocalStorage(
              thread, reinterpret_cast<void*>(static_cast<std::uintptr_t>(token.value())));
          if (tls_error != JVMTI_ERROR_NONE) {
            engine->quality().Add(QualityCounter::kThreadLocalStorageLoss);
            RecordJvmtiError(tls_error);
            static_cast<void>(engine->OnThreadEnd(token));
          } else {
            ++updated;
          }
        }
      } else {
        if (engine->UpdateThreadMetadata(token, metadata).ok()) {
          ++updated;
        } else {
          engine->quality().Add(QualityCounter::kThreadMetadataLoss);
          const auto clear_error = jvmti_->SetThreadLocalStorage(thread, nullptr);
          if (clear_error != JVMTI_ERROR_NONE) {
            RecordJvmtiError(clear_error);
          } else if (engine->OnThreadStart(metadata, &token).ok()) {
            const auto set_error = jvmti_->SetThreadLocalStorage(
                thread, reinterpret_cast<void*>(static_cast<std::uintptr_t>(token.value())));
            if (set_error == JVMTI_ERROR_NONE) {
              ++updated;
            } else {
              RecordJvmtiError(set_error);
              static_cast<void>(engine->OnThreadEnd(token));
            }
          }
        }
      }
    }
    jni->DeleteLocalRef(thread);
  }
  for (std::uint32_t index = limit; index < static_cast<std::uint32_t>(count); ++index) {
    jni->DeleteLocalRef(threads[index]);
  }
  if (threads != nullptr) static_cast<void>(jvmti_->Deallocate(
      reinterpret_cast<unsigned char*>(threads)));
  return updated;
}

ThreadMetadata ArtJvmtiAdapter::ReadThreadMetadata(
    JNIEnv* const jni, jthread thread, bool* const valid) noexcept {
  *valid = false;
  jvmtiThreadInfo info{};
  const auto info_error = jvmti_->GetThreadInfo(thread, &info);
  JvmtiString name(jvmti_, info.name);
  if (info_error != JVMTI_ERROR_NONE) {
    RecordJvmtiError(info_error);
    NativeEngine* const engine = bridge::BridgeRuntime::Instance().callback_engine();
    if (engine != nullptr) engine->quality().Add(QualityCounter::kThreadMetadataLoss);
    if (info.thread_group != nullptr) jni->DeleteLocalRef(info.thread_group);
    if (info.context_class_loader != nullptr) jni->DeleteLocalRef(info.context_class_loader);
    return {};
  }
  jint state = 0;
  const auto state_error = jvmti_->GetThreadState(thread, &state);
  if (state_error != JVMTI_ERROR_NONE) RecordJvmtiError(state_error);
  if (info.thread_group != nullptr) jni->DeleteLocalRef(info.thread_group);
  if (info.context_class_loader != nullptr) jni->DeleteLocalRef(info.context_class_loader);
  *valid = true;
  return ThreadMetadata{
      0U,
      HashBytes(name.get()),
      state_error == JVMTI_ERROR_NONE ? static_cast<std::uint32_t>(state) : 0U,
      0U,
      info.is_daemon != 0,
  };
}

ThreadToken ArtJvmtiAdapter::TokenFor(jthread thread) noexcept {
  if (jvmti_ == nullptr || thread == nullptr) return {};
  void* value = nullptr;
  const auto error = jvmti_->GetThreadLocalStorage(thread, &value);
  if (error != JVMTI_ERROR_NONE) {
    RecordJvmtiError(error);
    return {};
  }
  return ThreadToken(static_cast<std::uint64_t>(reinterpret_cast<std::uintptr_t>(value)));
}

ThreadToken ArtJvmtiAdapter::EnsureCallbackThreadToken(
    NativeEngine* const engine, jthread thread) noexcept {
  auto token = TokenFor(thread);
  if (token.valid() || engine == nullptr) return token;
  const ThreadMetadata metadata{CurrentTid(), 0U, 0U, 0U, false};
  if (!engine->OnThreadStart(metadata, &token).ok()) return {};
  const auto error = jvmti_->SetThreadLocalStorage(
      thread, reinterpret_cast<void*>(static_cast<std::uintptr_t>(token.value())));
  if (error != JVMTI_ERROR_NONE) {
    engine->quality().Add(QualityCounter::kThreadLocalStorageLoss);
    RecordJvmtiError(error);
    static_cast<void>(engine->OnThreadEnd(token));
    return {};
  }
  return token;
}

std::int32_t ArtJvmtiAdapter::RefreshThreadMetadata(JNIEnv* const jni) noexcept {
  std::lock_guard lock(control_mutex_);
  if (!attached_) return -static_cast<std::int32_t>(StatusCode::kInvalidState);
  const auto result = SnapshotThreads(jni);
  if (result > 0) {
    auto active = active_capabilities();
    active.add(Capability::kThreadMetadata);
    active_bits_.store(active.bits(), std::memory_order_release);
    PublishCapabilities();
  }
  return result;
}

std::int32_t ArtJvmtiAdapter::CaptureStack(
    JNIEnv*,
    jthread thread,
    const std::uint32_t trigger,
    const std::uint64_t context_token,
    const std::uint64_t related_sequence) noexcept {
  std::lock_guard lock(control_mutex_);
  return CaptureStackLocked(thread, trigger, context_token, related_sequence);
}

std::int32_t ArtJvmtiAdapter::CaptureStackForToken(
    JNIEnv* const jni,
    const std::uint64_t thread_token,
    const std::uint32_t trigger,
    const std::uint64_t context_token,
    const std::uint64_t related_sequence) noexcept {
  std::lock_guard lock(control_mutex_);
  if (!attached_ || jvmti_ == nullptr || jni == nullptr || thread_token == 0U) {
    return -static_cast<std::int32_t>(StatusCode::kInvalidState);
  }
  jint count = 0;
  jthread* threads = nullptr;
  const auto error = jvmti_->GetAllThreads(&count, &threads);
  if (error != JVMTI_ERROR_NONE || count < 0 || (count > 0 && threads == nullptr)) {
    RecordJvmtiError(error);
    if (threads != nullptr) static_cast<void>(jvmti_->Deallocate(
        reinterpret_cast<unsigned char*>(threads)));
    return -static_cast<std::int32_t>(StatusCode::kUnsupported);
  }
  std::int32_t result = -static_cast<std::int32_t>(StatusCode::kNotFound);
  const auto limit = std::min<std::uint32_t>(
      static_cast<std::uint32_t>(count), config_.max_tracked_threads);
  for (std::uint32_t index = 0U; index < static_cast<std::uint32_t>(count); ++index) {
    jthread thread = threads[index];
    if (index < limit && result < 0 && TokenFor(thread).value() == thread_token) {
      result = CaptureStackLocked(thread, trigger, context_token, related_sequence);
    }
    jni->DeleteLocalRef(thread);
  }
  if (threads != nullptr) static_cast<void>(jvmti_->Deallocate(
      reinterpret_cast<unsigned char*>(threads)));
  return result;
}

std::int32_t ArtJvmtiAdapter::LinkThreadContext(
    jthread thread, const std::uint64_t context_token) noexcept {
  if (thread == nullptr || context_token == 0U) {
    return -static_cast<std::int32_t>(StatusCode::kInvalidArgument);
  }
  CallbackScope operation(this);
  NativeEngine* const engine = operation.engine();
  if (engine == nullptr) return -static_cast<std::int32_t>(StatusCode::kClosed);
  const auto token = EnsureCallbackThreadToken(engine, thread);
  if (!token.valid()) return -static_cast<std::int32_t>(StatusCode::kNotFound);
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
    jthread thread,
    const std::uint32_t trigger,
    const std::uint64_t context_token,
    const std::uint64_t related_sequence) noexcept {
  NativeEngine* const engine = bridge::BridgeRuntime::Instance().callback_engine();
  if (!attached_ || jvmti_ == nullptr || engine == nullptr || thread == nullptr ||
      !active_capabilities().contains(Capability::kStackTrace) || trigger == 0U || trigger > 3U) {
    return -static_cast<std::int32_t>(StatusCode::kInvalidState);
  }
  const auto max_per_minute = config_.max_stack_samples_per_minute;
  const auto min_interval_ns =
      static_cast<std::uint64_t>(config_.min_stack_trigger_interval_ms) * 1'000'000U;
  const auto now_ns = static_cast<std::uint64_t>(std::chrono::duration_cast<std::chrono::nanoseconds>(
      std::chrono::steady_clock::now().time_since_epoch()).count());
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
  const auto depth = static_cast<jint>(std::min<std::uint32_t>(config_.max_stack_depth, frames.size()));
  const auto error = jvmti_->GetStackTrace(thread, 0, depth, frames.data(), &frame_count);
  if (error != JVMTI_ERROR_NONE || frame_count < 0 || frame_count > depth) {
    engine->quality().Add(QualityCounter::kStackCaptureFailure);
    RecordJvmtiError(error);
    if (error == JVMTI_ERROR_NOT_AVAILABLE || error == JVMTI_ERROR_MUST_POSSESS_CAPABILITY) {
      RemoveActiveCapability(Capability::kStackTrace);
      PublishCapabilities();
    }
    return -static_cast<std::int32_t>(StatusCode::kUnsupported);
  }
  std::uint64_t fingerprint = 1469598103934665603ULL;
  for (jint index = 0; index < frame_count; ++index) {
    HashU64(
        static_cast<std::uint64_t>(reinterpret_cast<std::uintptr_t>(
            frames[static_cast<std::size_t>(index)].method)),
        &fingerprint);
    HashU64(static_cast<std::uint64_t>(frames[static_cast<std::size_t>(index)].location), &fingerprint);
  }
  if (fingerprint == 0U) fingerprint = 1U;
  const auto insert = stack_fingerprints_.Insert(fingerprint);
  if (insert == BoundedIdInsertResult::kFull) {
    engine->quality().Add(QualityCounter::kStackDefinitionCapacityLoss);
  } else if (insert == BoundedIdInsertResult::kInserted) {
    for (jint index = 0; index < frame_count; ++index) {
      NativeEvent definition{};
      definition.type = EventType::kStackDefinition;
      definition.context_token = context_token;
      definition.payload.status.value0 = fingerprint;
      definition.payload.status.value1 = static_cast<std::uint64_t>(reinterpret_cast<std::uintptr_t>(
          frames[static_cast<std::size_t>(index)].method));
      definition.payload.status.value2 = static_cast<std::uint64_t>(
          frames[static_cast<std::size_t>(index)].location);
      definition.payload.status.value3 = static_cast<std::uint64_t>(static_cast<std::uint32_t>(index)) |
          (static_cast<std::uint64_t>(static_cast<std::uint32_t>(frame_count)) << 32U);
      if (!engine->Publish(definition).ok()) {
        engine->quality().Add(QualityCounter::kStackDefinitionPublishLoss);
        break;
      }
    }
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

std::int32_t ArtJvmtiAdapter::ResolveMethod(
    JNIEnv* const jni,
    const std::uint64_t method_id,
    const std::span<std::byte> output) noexcept {
  std::lock_guard lock(control_mutex_);
  if (!attached_ || jvmti_ == nullptr || jni == nullptr || method_id == 0U ||
      output.size() < protocol::kMethodHeaderSize) {
    return -static_cast<std::int32_t>(StatusCode::kInvalidArgument);
  }
  const auto method = reinterpret_cast<jmethodID>(static_cast<std::uintptr_t>(method_id));
  char* method_name_raw = nullptr;
  char* method_signature_raw = nullptr;
  char* method_generic_raw = nullptr;
  auto error = jvmti_->GetMethodName(
      method, &method_name_raw, &method_signature_raw, &method_generic_raw);
  JvmtiString method_name(jvmti_, method_name_raw);
  JvmtiString method_signature(jvmti_, method_signature_raw);
  JvmtiString method_generic(jvmti_, method_generic_raw);
  if (error != JVMTI_ERROR_NONE) {
    RecordJvmtiError(error);
    return -static_cast<std::int32_t>(StatusCode::kNotFound);
  }
  jclass declaring_class = nullptr;
  error = jvmti_->GetMethodDeclaringClass(method, &declaring_class);
  if (error != JVMTI_ERROR_NONE || declaring_class == nullptr) {
    RecordJvmtiError(error);
    if (declaring_class != nullptr) jni->DeleteLocalRef(declaring_class);
    return -static_cast<std::int32_t>(StatusCode::kNotFound);
  }
  char* class_signature_raw = nullptr;
  char* class_generic_raw = nullptr;
  error = jvmti_->GetClassSignature(declaring_class, &class_signature_raw, &class_generic_raw);
  JvmtiString class_signature(jvmti_, class_signature_raw);
  JvmtiString class_generic(jvmti_, class_generic_raw);
  jni->DeleteLocalRef(declaring_class);
  if (error != JVMTI_ERROR_NONE || class_signature.get() == nullptr || method_name.get() == nullptr ||
      method_signature.get() == nullptr) {
    RecordJvmtiError(error);
    return -static_cast<std::int32_t>(StatusCode::kNotFound);
  }
  const auto class_length = strlen(class_signature.get());
  const auto name_length = strlen(method_name.get());
  const auto signature_length = strlen(method_signature.get());
  if (class_length > protocol::kMaxClassSignatureBytes ||
      name_length > protocol::kMaxMethodNameBytes ||
      signature_length > protocol::kMaxMethodSignatureBytes) {
    NativeEngine* const engine = bridge::BridgeRuntime::Instance().callback_engine();
    if (engine != nullptr) engine->quality().Add(QualityCounter::kMethodResolutionFailure);
    return -static_cast<std::int32_t>(StatusCode::kCapacityExhausted);
  }
  if (method_ids_.Insert(method_id) == BoundedIdInsertResult::kFull) {
    NativeEngine* const engine = bridge::BridgeRuntime::Instance().callback_engine();
    if (engine != nullptr) engine->quality().Add(QualityCounter::kMethodResolutionFailure);
    return -static_cast<std::int32_t>(StatusCode::kCapacityExhausted);
  }
  const auto total_size = static_cast<std::size_t>(protocol::kMethodHeaderSize) +
      class_length + name_length + signature_length;
  if (total_size > output.size() || total_size > protocol::kMaxMethodDefinitionBytes) {
    return -static_cast<std::int32_t>(StatusCode::kBufferTooSmall);
  }
  WriteU32(output, 0U, protocol::kMethodMagic);
  WriteU16(output, 4U, protocol::kMethodProtocolVersion);
  WriteU16(output, 6U, protocol::kMethodHeaderSize);
  WriteU32(output, 8U, static_cast<std::uint32_t>(total_size));
  WriteU32(output, 12U, 0U);
  WriteU64(output, 16U, method_id);
  WriteU16(output, 24U, static_cast<std::uint16_t>(class_length));
  WriteU16(output, 26U, static_cast<std::uint16_t>(name_length));
  WriteU16(output, 28U, static_cast<std::uint16_t>(signature_length));
  WriteU16(output, 30U, 0U);
  auto* cursor = output.data() + protocol::kMethodHeaderSize;
  memcpy(cursor, class_signature.get(), class_length);
  cursor += class_length;
  memcpy(cursor, method_name.get(), name_length);
  cursor += name_length;
  memcpy(cursor, method_signature.get(), signature_length);
  return static_cast<std::int32_t>(total_size);
}

void ArtJvmtiAdapter::PublishStatus(const std::uint64_t status, const std::uint64_t detail) noexcept {
  NativeEngine* const engine = bridge::BridgeRuntime::Instance().callback_engine();
  if (engine == nullptr) return;
  NativeEvent event{};
  event.type = EventType::kAgentStatus;
  event.payload.status.value0 = status;
  event.payload.status.value1 = detail;
  event.payload.status.value2 = config_.config_hash;
  event.payload.status.value3 = engine->MemoryBytes() + stack_fingerprints_.MemoryBytes() +
      method_ids_.MemoryBytes();
  static_cast<void>(engine->Publish(event));
}

void ArtJvmtiAdapter::PublishCapabilities() noexcept {
  NativeEngine* const engine = bridge::BridgeRuntime::Instance().callback_engine();
  if (engine == nullptr) return;
  NativeEvent event{};
  event.type = EventType::kCapability;
  event.payload.status.value0 = requested_.bits();
  event.payload.status.value1 = potential_.bits();
  event.payload.status.value2 = granted_.bits();
  event.payload.status.value3 = active_bits_.load(std::memory_order_acquire);
  static_cast<void>(engine->Publish(event));
}

void ArtJvmtiAdapter::RemoveActiveCapability(const Capability capability) noexcept {
  auto bits = active_bits_.load(std::memory_order_acquire);
  const auto mask = ~static_cast<std::uint64_t>(capability);
  while (!active_bits_.compare_exchange_weak(
      bits, bits & mask, std::memory_order_acq_rel, std::memory_order_acquire)) {
  }
}

void ArtJvmtiAdapter::RecordJvmtiError(const jvmtiError error) noexcept {
  if (error == JVMTI_ERROR_NONE) return;
  last_error_.store(error, std::memory_order_relaxed);
  NativeEngine* const engine = bridge::BridgeRuntime::Instance().callback_engine();
  if (engine != nullptr) engine->quality().Add(QualityCounter::kJvmtiError);
}

void ArtJvmtiAdapter::OnGcStart() noexcept {
  CallbackScope callback(this);
  if (callback.engine() != nullptr) static_cast<void>(callback.engine()->OnGcStart());
}

void ArtJvmtiAdapter::OnGcFinish() noexcept {
  CallbackScope callback(this);
  if (callback.engine() != nullptr) static_cast<void>(callback.engine()->OnGcFinish());
}

void ArtJvmtiAdapter::OnThreadStart(jthread thread) noexcept {
  CallbackScope callback(this);
  NativeEngine* const engine = callback.engine();
  if (engine == nullptr) return;
  static_cast<void>(EnsureCallbackThreadToken(engine, thread));
}

void ArtJvmtiAdapter::OnThreadEnd(jthread thread) noexcept {
  CallbackScope callback(this);
  NativeEngine* const engine = callback.engine();
  if (engine == nullptr) return;
  const auto token = TokenFor(thread);
  if (token.valid()) {
    static_cast<void>(engine->OnThreadEnd(token));
    const auto error = jvmti_->SetThreadLocalStorage(thread, nullptr);
    if (error != JVMTI_ERROR_NONE) RecordJvmtiError(error);
  } else {
    engine->quality().Add(QualityCounter::kThreadUnknownEnd);
  }
}

void ArtJvmtiAdapter::OnMonitorEnter(jthread thread) noexcept {
  CallbackScope callback(this);
  NativeEngine* const engine = callback.engine();
  if (engine == nullptr) return;
  const auto token = EnsureCallbackThreadToken(engine, thread);
  if (token.valid()) static_cast<void>(engine->OnMonitorEnter(token));
}

void ArtJvmtiAdapter::OnMonitorEntered(jthread thread) noexcept {
  CallbackScope callback(this);
  NativeEngine* const engine = callback.engine();
  if (engine == nullptr) return;
  const auto token = EnsureCallbackThreadToken(engine, thread);
  if (token.valid()) static_cast<void>(engine->OnMonitorEntered(token));
}

void JNICALL ArtJvmtiAdapter::GcStartCallback(jvmtiEnv*) noexcept { Instance().OnGcStart(); }
void JNICALL ArtJvmtiAdapter::GcFinishCallback(jvmtiEnv*) noexcept { Instance().OnGcFinish(); }
void JNICALL ArtJvmtiAdapter::ThreadStartCallback(jvmtiEnv*, JNIEnv*, jthread thread) noexcept {
  Instance().OnThreadStart(thread);
}
void JNICALL ArtJvmtiAdapter::ThreadEndCallback(jvmtiEnv*, JNIEnv*, jthread thread) noexcept {
  Instance().OnThreadEnd(thread);
}
void JNICALL ArtJvmtiAdapter::MonitorEnterCallback(jvmtiEnv*, JNIEnv*, jthread thread, jobject) noexcept {
  Instance().OnMonitorEnter(thread);
}
void JNICALL ArtJvmtiAdapter::MonitorEnteredCallback(jvmtiEnv*, JNIEnv*, jthread thread, jobject) noexcept {
  Instance().OnMonitorEntered(thread);
}

}  // namespace jankhunter::artti::art

extern "C" JNIEXPORT jint JNICALL Agent_OnAttach(JavaVM* vm, char* options, void*) {
  jankhunter::artti::bridge::ArtTiNativeConfigV1 config{};
  if (!jankhunter::artti::art::ParseAgentOptions(options, &config).ok()) return JNI_ERR;
  void* environment = nullptr;
  if (vm == nullptr || vm->GetEnv(&environment, JVMTI_VERSION_1_2) != JNI_OK || environment == nullptr) {
    return JNI_ERR;
  }
  auto& bridge = jankhunter::artti::bridge::BridgeRuntime::Instance();
  if (!bridge.Initialize(config).ok()) return JNI_ERR;
  const auto status = jankhunter::artti::art::ArtJvmtiAdapter::Instance().Attach(
      vm, static_cast<jvmtiEnv*>(environment), config);
  if (!status.ok()) {
    static_cast<void>(bridge.Stop());
    return JNI_ERR;
  }
  return JNI_OK;
}

extern "C" JNIEXPORT jint JNICALL Agent_OnLoad(JavaVM* vm, char* options, void* reserved) {
  return Agent_OnAttach(vm, options, reserved);
}

extern "C" JNIEXPORT void JNICALL Agent_OnUnload(JavaVM*) {
  const auto status = jankhunter::artti::art::ArtJvmtiAdapter::Instance().Stop(500U);
  if (status.ok()) {
    static_cast<void>(jankhunter::artti::bridge::BridgeRuntime::Instance().Stop());
  }
}
