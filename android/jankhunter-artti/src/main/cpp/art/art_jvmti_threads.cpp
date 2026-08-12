#include "art/art_jvmti_adapter.h"

#include <sys/syscall.h>
#include <unistd.h>

#include <algorithm>

#include "art/jvmti_resources.h"
#include "bridge/bridge_runtime.h"

namespace jankhunter::artti::art {
namespace {

std::uint64_t CurrentTid() noexcept {
  const auto tid = syscall(SYS_gettid);
  return tid <= 0 ? 0U : static_cast<std::uint64_t>(tid);
}

std::uint64_t HashBytes(const char *value) noexcept {
  if (value == nullptr)
    return 0U;
  std::uint64_t hash = 1469598103934665603ULL;
  for (const auto *cursor = reinterpret_cast<const unsigned char *>(value);
       *cursor != 0U; ++cursor) {
    hash ^= *cursor;
    hash *= 1099511628211ULL;
  }
  return hash == 0U ? 1U : hash;
}

} // namespace

std::int32_t ArtJvmtiAdapter::SnapshotThreads(JNIEnv *const jni) noexcept {
  if (jni == nullptr || jvmti_ == nullptr) {
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

  NativeEngine *const engine =
      bridge::BridgeRuntime::Instance().callback_engine();
  std::int32_t updated = 0;
  const auto limit = std::min<std::uint32_t>(static_cast<std::uint32_t>(count),
                                             config_.max_tracked_threads);
  for (std::uint32_t index = 0U; index < static_cast<std::uint32_t>(count);
       ++index) {
    jthread thread = threads[index];
    JniLocalRef<jthread> thread_ref(jni, thread);
    if (index >= limit)
      continue;

    bool valid = false;
    const auto metadata = ReadThreadMetadata(jni, thread, &valid);
    if (!valid || engine == nullptr)
      continue;

    auto token = TokenFor(thread);
    if (!token.valid()) {
      if (engine->OnThreadStart(metadata, &token).ok()) {
        const auto tls_error = jvmti_->SetThreadLocalStorage(
            thread, reinterpret_cast<void *>(
                        static_cast<std::uintptr_t>(token.value())));
        if (tls_error == JVMTI_ERROR_NONE) {
          ++updated;
        } else {
          engine->quality().Add(QualityCounter::kThreadLocalStorageLoss);
          RecordJvmtiError(tls_error);
          static_cast<void>(engine->OnThreadEnd(token));
        }
      }
      continue;
    }

    if (engine->UpdateThreadMetadata(token, metadata).ok()) {
      ++updated;
      continue;
    }

    engine->quality().Add(QualityCounter::kThreadMetadataLoss);
    const auto clear_error = jvmti_->SetThreadLocalStorage(thread, nullptr);
    if (clear_error != JVMTI_ERROR_NONE) {
      RecordJvmtiError(clear_error);
      continue;
    }
    if (!engine->OnThreadStart(metadata, &token).ok())
      continue;

    const auto set_error = jvmti_->SetThreadLocalStorage(
        thread,
        reinterpret_cast<void *>(static_cast<std::uintptr_t>(token.value())));
    if (set_error == JVMTI_ERROR_NONE) {
      ++updated;
    } else {
      RecordJvmtiError(set_error);
      static_cast<void>(engine->OnThreadEnd(token));
    }
  }
  return updated;
}

ThreadMetadata ArtJvmtiAdapter::ReadThreadMetadata(JNIEnv *const jni,
                                                   jthread thread,
                                                   bool *const valid) noexcept {
  *valid = false;
  jvmtiThreadInfo info{};
  const auto info_error = jvmti_->GetThreadInfo(thread, &info);
  JvmtiAllocation<char> name(jvmti_, info.name);
  JniLocalRef<jthreadGroup> thread_group(jni, info.thread_group);
  JniLocalRef<jobject> class_loader(jni, info.context_class_loader);
  if (info_error != JVMTI_ERROR_NONE) {
    RecordJvmtiError(info_error);
    NativeEngine *const engine =
        bridge::BridgeRuntime::Instance().callback_engine();
    if (engine != nullptr)
      engine->quality().Add(QualityCounter::kThreadMetadataLoss);
    return {};
  }

  jint state = 0;
  const auto state_error = jvmti_->GetThreadState(thread, &state);
  if (state_error != JVMTI_ERROR_NONE)
    RecordJvmtiError(state_error);
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
  if (jvmti_ == nullptr || thread == nullptr)
    return {};
  void *value = nullptr;
  const auto error = jvmti_->GetThreadLocalStorage(thread, &value);
  if (error != JVMTI_ERROR_NONE) {
    RecordJvmtiError(error);
    return {};
  }
  return ThreadToken(
      static_cast<std::uint64_t>(reinterpret_cast<std::uintptr_t>(value)));
}

ThreadToken
ArtJvmtiAdapter::EnsureCallbackThreadToken(NativeEngine *const engine,
                                           jthread thread) noexcept {
  auto token = TokenFor(thread);
  if (token.valid() || engine == nullptr)
    return token;
  const ThreadMetadata metadata{CurrentTid(), 0U, 0U, 0U, false};
  if (!engine->OnThreadStart(metadata, &token).ok())
    return {};
  const auto error = jvmti_->SetThreadLocalStorage(
      thread,
      reinterpret_cast<void *>(static_cast<std::uintptr_t>(token.value())));
  if (error != JVMTI_ERROR_NONE) {
    engine->quality().Add(QualityCounter::kThreadLocalStorageLoss);
    RecordJvmtiError(error);
    static_cast<void>(engine->OnThreadEnd(token));
    return {};
  }
  return token;
}

std::int32_t
ArtJvmtiAdapter::RefreshThreadMetadata(JNIEnv *const jni) noexcept {
  std::lock_guard lock(control_mutex_);
  if (!attached_)
    return -static_cast<std::int32_t>(StatusCode::kInvalidState);
  const auto result = SnapshotThreads(jni);
  if (result > 0) {
    auto active = active_capabilities();
    active.add(Capability::kThreadMetadata);
    active_bits_.store(active.bits(), std::memory_order_release);
    PublishCapabilities();
  }
  return result;
}

} // namespace jankhunter::artti::art
