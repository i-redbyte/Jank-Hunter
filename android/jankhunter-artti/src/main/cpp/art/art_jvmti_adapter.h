#ifndef JANKHUNTER_ARTTI_ART_ART_JVMTI_ADAPTER_H_
#define JANKHUNTER_ARTTI_ART_ART_JVMTI_ADAPTER_H_

#include <jni.h>
#include <jvmti.h>

#include <array>
#include <atomic>
#include <cstddef>
#include <cstdint>
#include <memory>
#include <mutex>
#include <span>

#include "bridge/artti_abi.h"
#include "core/bounded_id_table.h"
#include "core/capability.h"
#include "core/engine.h"
#include "core/status.h"

namespace jankhunter::artti::art {

class ArtJvmtiAdapter final {
 public:
  [[nodiscard]] static ArtJvmtiAdapter& Instance() noexcept;

  [[nodiscard]] Status Attach(
      JavaVM* vm, jvmtiEnv* jvmti, const bridge::ArtTiNativeConfigV1& config) noexcept;
  [[nodiscard]] Status Stop(std::uint32_t timeout_ms) noexcept;
  [[nodiscard]] std::int32_t RefreshThreadMetadata(JNIEnv* jni) noexcept;
  [[nodiscard]] std::int32_t CaptureStack(
      JNIEnv* jni,
      jthread thread,
      std::uint32_t trigger,
      std::uint64_t context_token,
      std::uint64_t related_sequence) noexcept;
  [[nodiscard]] std::int32_t ResolveMethod(
      JNIEnv* jni, std::uint64_t method_id, std::span<std::byte> output) noexcept;

  [[nodiscard]] CapabilitySet requested_capabilities() const noexcept { return requested_; }
  [[nodiscard]] CapabilitySet potential_capabilities() const noexcept { return potential_; }
  [[nodiscard]] CapabilitySet granted_capabilities() const noexcept { return granted_; }
  [[nodiscard]] CapabilitySet active_capabilities() const noexcept {
    return CapabilitySet(active_bits_.load(std::memory_order_acquire));
  }

 private:
  ArtJvmtiAdapter() = default;

  class CallbackScope final {
   public:
    explicit CallbackScope(ArtJvmtiAdapter* adapter) noexcept;
    ~CallbackScope();
    CallbackScope(const CallbackScope&) = delete;
    CallbackScope& operator=(const CallbackScope&) = delete;
    [[nodiscard]] NativeEngine* engine() const noexcept { return engine_; }

   private:
    ArtJvmtiAdapter* adapter_{nullptr};
    NativeEngine* engine_{nullptr};
  };

  [[nodiscard]] Status ConfigureCapabilities() noexcept;
  [[nodiscard]] Status ConfigureCallbacks() noexcept;
  [[nodiscard]] bool EnablePair(jvmtiEvent first, jvmtiEvent second) noexcept;
  [[nodiscard]] bool EnableSingle(jvmtiEvent event) noexcept;
  void DisableConfiguredEvents() noexcept;
  [[nodiscard]] std::int32_t SnapshotThreads(JNIEnv* jni) noexcept;
  [[nodiscard]] ThreadMetadata ReadThreadMetadata(JNIEnv* jni, jthread thread, bool* valid) noexcept;
  [[nodiscard]] ThreadToken TokenFor(jthread thread) noexcept;
  [[nodiscard]] ThreadToken EnsureCallbackThreadToken(NativeEngine* engine, jthread thread) noexcept;
  void PublishStatus(std::uint64_t status, std::uint64_t detail) noexcept;
  void PublishCapabilities() noexcept;
  void RemoveActiveCapability(Capability capability) noexcept;
  void RecordJvmtiError(jvmtiError error) noexcept;

  void OnGcStart() noexcept;
  void OnGcFinish() noexcept;
  void OnThreadStart(jthread thread) noexcept;
  void OnThreadEnd(jthread thread) noexcept;
  void OnMonitorEnter(jthread thread) noexcept;
  void OnMonitorEntered(jthread thread) noexcept;

  static void JNICALL GcStartCallback(jvmtiEnv*) noexcept;
  static void JNICALL GcFinishCallback(jvmtiEnv*) noexcept;
  static void JNICALL ThreadStartCallback(jvmtiEnv*, JNIEnv*, jthread) noexcept;
  static void JNICALL ThreadEndCallback(jvmtiEnv*, JNIEnv*, jthread) noexcept;
  static void JNICALL MonitorEnterCallback(jvmtiEnv*, JNIEnv*, jthread, jobject) noexcept;
  static void JNICALL MonitorEnteredCallback(jvmtiEnv*, JNIEnv*, jthread, jobject) noexcept;

  std::mutex control_mutex_;
  JavaVM* vm_{nullptr};
  jvmtiEnv* jvmti_{nullptr};
  NativeConfigSnapshot config_{};
  BoundedIdTable stack_fingerprints_;
  BoundedIdTable method_ids_;
  CapabilitySet requested_{};
  CapabilitySet potential_{};
  CapabilitySet granted_{};
  std::atomic<std::uint64_t> active_bits_{0U};
  std::atomic<bool> accepting_callbacks_{false};
  std::atomic<std::uint32_t> active_callbacks_{0U};
  std::atomic<jvmtiError> last_error_{JVMTI_ERROR_NONE};
  std::uint64_t stack_budget_window_start_ns_{0U};
  std::uint64_t last_stack_capture_ns_{0U};
  std::uint32_t stack_captures_in_window_{0U};
  std::array<jvmtiEvent, 6U> enabled_events_{};
  std::uint32_t enabled_event_count_{0U};
  bool attached_{false};
};

}  // namespace jankhunter::artti::art

#endif  // JANKHUNTER_ARTTI_ART_ART_JVMTI_ADAPTER_H_
