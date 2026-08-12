#include "art/art_jvmti_adapter.h"

#include <cstring>

#include "art/jvmti_resources.h"
#include "bridge/bridge_runtime.h"
#include "protocol/method_wire.h"

namespace jankhunter::artti::art {
namespace {

void WriteU16(const std::span<std::byte> output, const std::size_t offset,
              const std::uint16_t value) noexcept {
  output[offset] = static_cast<std::byte>(value & 0xFFU);
  output[offset + 1U] = static_cast<std::byte>((value >> 8U) & 0xFFU);
}

void WriteU32(const std::span<std::byte> output, const std::size_t offset,
              const std::uint32_t value) noexcept {
  for (std::size_t index = 0U; index < sizeof(value); ++index) {
    output[offset + index] =
        static_cast<std::byte>((value >> (index * 8U)) & 0xFFU);
  }
}

void WriteU64(const std::span<std::byte> output, const std::size_t offset,
              const std::uint64_t value) noexcept {
  for (std::size_t index = 0U; index < sizeof(value); ++index) {
    output[offset + index] =
        static_cast<std::byte>((value >> (index * 8U)) & 0xFFU);
  }
}

void RecordResolutionFailure() noexcept {
  NativeEngine *const engine =
      bridge::BridgeRuntime::Instance().callback_engine();
  if (engine != nullptr)
    engine->quality().Add(QualityCounter::kMethodResolutionFailure);
}

} // namespace

std::int32_t
ArtJvmtiAdapter::ResolveMethod(JNIEnv *const jni, const std::uint64_t method_id,
                               const std::span<std::byte> output) noexcept {
  std::lock_guard lock(control_mutex_);
  if (!attached_ || jvmti_ == nullptr || jni == nullptr || method_id == 0U ||
      output.size() < protocol::kMethodHeaderSize) {
    return -static_cast<std::int32_t>(StatusCode::kInvalidArgument);
  }

  const auto method =
      reinterpret_cast<jmethodID>(static_cast<std::uintptr_t>(method_id));
  JvmtiAllocation<char> method_name(jvmti_);
  JvmtiAllocation<char> method_signature(jvmti_);
  JvmtiAllocation<char> method_generic(jvmti_);
  auto error = jvmti_->GetMethodName(
      method, method_name.out(), method_signature.out(), method_generic.out());
  if (error != JVMTI_ERROR_NONE) {
    RecordJvmtiError(error);
    return -static_cast<std::int32_t>(StatusCode::kNotFound);
  }

  jclass declaring_class_raw = nullptr;
  error = jvmti_->GetMethodDeclaringClass(method, &declaring_class_raw);
  JniLocalRef<jclass> declaring_class(jni, declaring_class_raw);
  if (error != JVMTI_ERROR_NONE || declaring_class.get() == nullptr) {
    RecordJvmtiError(error);
    return -static_cast<std::int32_t>(StatusCode::kNotFound);
  }

  JvmtiAllocation<char> class_signature(jvmti_);
  JvmtiAllocation<char> class_generic(jvmti_);
  error = jvmti_->GetClassSignature(declaring_class.get(),
                                    class_signature.out(), class_generic.out());
  if (error != JVMTI_ERROR_NONE || class_signature.get() == nullptr ||
      method_name.get() == nullptr || method_signature.get() == nullptr) {
    RecordJvmtiError(error);
    return -static_cast<std::int32_t>(StatusCode::kNotFound);
  }

  const auto class_length = std::strlen(class_signature.get());
  const auto name_length = std::strlen(method_name.get());
  const auto signature_length = std::strlen(method_signature.get());
  if (class_length > protocol::kMaxClassSignatureBytes ||
      name_length > protocol::kMaxMethodNameBytes ||
      signature_length > protocol::kMaxMethodSignatureBytes) {
    RecordResolutionFailure();
    return -static_cast<std::int32_t>(StatusCode::kCapacityExhausted);
  }

  const auto total_size =
      static_cast<std::size_t>(protocol::kMethodHeaderSize) + class_length +
      name_length + signature_length;
  if (total_size > output.size() ||
      total_size > protocol::kMaxMethodDefinitionBytes) {
    return -static_cast<std::int32_t>(StatusCode::kBufferTooSmall);
  }

  const auto method_insert = method_ids_.Insert(method_id);
  if (method_insert == BoundedIdInsertResult::kFull) {
    RecordResolutionFailure();
    return -static_cast<std::int32_t>(StatusCode::kCapacityExhausted);
  }
  if (method_insert == BoundedIdInsertResult::kExisting)
    return 0;

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

  auto *cursor = output.data() + protocol::kMethodHeaderSize;
  std::memcpy(cursor, class_signature.get(), class_length);
  cursor += class_length;
  std::memcpy(cursor, method_name.get(), name_length);
  cursor += name_length;
  std::memcpy(cursor, method_signature.get(), signature_length);
  return static_cast<std::int32_t>(total_size);
}

} // namespace jankhunter::artti::art
