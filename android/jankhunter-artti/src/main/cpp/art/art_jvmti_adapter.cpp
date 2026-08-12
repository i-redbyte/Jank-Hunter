#include "art/art_jvmti_adapter.h"

#include <chrono>
#include <thread>

#include "art/agent_options.h"
#include "art/capability_negotiator.h"
#include "bridge/bridge_runtime.h"

namespace jankhunter::artti::art {
namespace {

constexpr std::uint64_t kStatusAttachStarted = 1U;
constexpr std::uint64_t kStatusAttachSucceeded = 2U;
constexpr std::uint64_t kStatusAttachDegraded = 3U;
constexpr std::uint64_t kStatusAttachFailed = 4U;
constexpr std::uint64_t kStatusStopped = 5U;
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
