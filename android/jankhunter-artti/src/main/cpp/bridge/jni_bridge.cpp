#include <jni.h>

#include <cstddef>
#include <cstdint>
#include <cstring>
#include <span>

#include "bridge/artti_abi.h"
#include "bridge/bridge_runtime.h"
#include "art/art_jvmti_adapter.h"
#include "core/status.h"

namespace {

using jankhunter::artti::StatusCode;
using jankhunter::artti::bridge::ArtTiHandshakeV1;
using jankhunter::artti::bridge::ArtTiNativeConfigV1;
using jankhunter::artti::bridge::BridgeRuntime;

template <typename T>
T* DirectBuffer(JNIEnv* env, jobject buffer, std::size_t required_bytes) noexcept {
  if (env == nullptr || buffer == nullptr) return nullptr;
  void* const address = env->GetDirectBufferAddress(buffer);
  const jlong capacity = env->GetDirectBufferCapacity(buffer);
  if (address == nullptr || capacity < 0 || static_cast<std::uint64_t>(capacity) < required_bytes) {
    return nullptr;
  }
  return static_cast<T*>(address);
}

jint Code(const jankhunter::artti::Status status) noexcept {
  return static_cast<jint>(status.code);
}

}  // namespace

extern "C" JNIEXPORT jint JNICALL
Java_io_jankhunter_artti_internal_ArtTiNativeBridge_nativeHandshake(
    JNIEnv* env, jobject, jobject response_buffer) noexcept {
  auto* const output = DirectBuffer<std::byte>(env, response_buffer, sizeof(ArtTiHandshakeV1));
  if (output == nullptr) return static_cast<jint>(StatusCode::kInvalidArgument);
  const auto handshake = BridgeRuntime::Instance().Handshake();
  std::memcpy(output, &handshake, sizeof(handshake));
  return static_cast<jint>(StatusCode::kOk);
}

extern "C" JNIEXPORT jint JNICALL
Java_io_jankhunter_artti_internal_ArtTiNativeBridge_nativeInitialize(
    JNIEnv* env, jobject, jobject config_buffer) noexcept {
  const auto* const input = DirectBuffer<ArtTiNativeConfigV1>(
      env, config_buffer, sizeof(ArtTiNativeConfigV1));
  if (input == nullptr) return static_cast<jint>(StatusCode::kInvalidArgument);
  ArtTiNativeConfigV1 config{};
  std::memcpy(&config, input, sizeof(config));
  return Code(BridgeRuntime::Instance().Initialize(config));
}

extern "C" JNIEXPORT jint JNICALL
Java_io_jankhunter_artti_internal_ArtTiNativeBridge_nativeDrain(
    JNIEnv* env, jobject, jobject output_buffer, jint max_records) noexcept {
  if (env == nullptr || output_buffer == nullptr || max_records < 0) {
    return -static_cast<jint>(StatusCode::kInvalidArgument);
  }
  void* const address = env->GetDirectBufferAddress(output_buffer);
  const jlong capacity = env->GetDirectBufferCapacity(output_buffer);
  if (address == nullptr || capacity < 0) return -static_cast<jint>(StatusCode::kInvalidArgument);
  const auto result = BridgeRuntime::Instance().Drain(
      std::span<std::byte>(
          static_cast<std::byte*>(address), static_cast<std::size_t>(capacity)),
      static_cast<std::uint32_t>(max_records));
  return result.status.ok()
      ? static_cast<jint>(result.bytes_written)
      : -static_cast<jint>(result.status.code);
}

extern "C" JNIEXPORT jint JNICALL
Java_io_jankhunter_artti_internal_ArtTiNativeBridge_nativeStop(JNIEnv*, jobject) noexcept {
  const auto adapter_status = jankhunter::artti::art::ArtJvmtiAdapter::Instance().Stop(500U);
  if (!adapter_status.ok()) return Code(adapter_status);
  return Code(BridgeRuntime::Instance().Stop());
}

extern "C" JNIEXPORT jint JNICALL
Java_io_jankhunter_artti_internal_ArtTiNativeBridge_nativeRefreshThreadMetadata(
    JNIEnv* env, jobject) noexcept {
  return jankhunter::artti::art::ArtJvmtiAdapter::Instance().RefreshThreadMetadata(env);
}

extern "C" JNIEXPORT jint JNICALL
Java_io_jankhunter_artti_internal_ArtTiNativeBridge_nativeCaptureStack(
    JNIEnv* env,
    jobject,
    jobject thread,
    jint trigger,
    jlong context_token,
    jlong related_sequence) noexcept {
  if (trigger <= 0) {
    return -static_cast<jint>(StatusCode::kInvalidArgument);
  }
  return jankhunter::artti::art::ArtJvmtiAdapter::Instance().CaptureStack(
      env,
      static_cast<jthread>(thread),
      static_cast<std::uint32_t>(trigger),
      static_cast<std::uint64_t>(context_token),
      static_cast<std::uint64_t>(related_sequence));
}

extern "C" JNIEXPORT jint JNICALL
Java_io_jankhunter_artti_internal_ArtTiNativeBridge_nativeResolveMethod(
    JNIEnv* env, jobject, jlong method_id, jobject output_buffer) noexcept {
  if (method_id <= 0 || env == nullptr || output_buffer == nullptr) {
    return -static_cast<jint>(StatusCode::kInvalidArgument);
  }
  void* const address = env->GetDirectBufferAddress(output_buffer);
  const jlong capacity = env->GetDirectBufferCapacity(output_buffer);
  if (address == nullptr || capacity < 0) return -static_cast<jint>(StatusCode::kInvalidArgument);
  return jankhunter::artti::art::ArtJvmtiAdapter::Instance().ResolveMethod(
      env,
      static_cast<std::uint64_t>(method_id),
      std::span<std::byte>(static_cast<std::byte*>(address), static_cast<std::size_t>(capacity)));
}

extern "C" JNIEXPORT jint JNICALL
Java_io_jankhunter_artti_internal_ArtTiNativeBridge_nativePublishSynthetic(
    JNIEnv*, jobject, jint event_type, jint count) noexcept {
  if (event_type < 0 || event_type > UINT16_MAX || count < 0) {
    return -static_cast<jint>(StatusCode::kInvalidArgument);
  }
  return BridgeRuntime::Instance().PublishSynthetic(
      static_cast<std::uint16_t>(event_type), static_cast<std::uint32_t>(count));
}
