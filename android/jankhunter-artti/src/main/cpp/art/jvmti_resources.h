#ifndef JANKHUNTER_ARTTI_ART_JVMTI_RESOURCES_H_
#define JANKHUNTER_ARTTI_ART_JVMTI_RESOURCES_H_

#include <jni.h>
#include <jvmti.h>

#include <cstddef>

namespace jankhunter::artti::art {

template <typename T> class JvmtiAllocation final {
public:
  explicit JvmtiAllocation(jvmtiEnv *env, T *value = nullptr) noexcept
      : env_(env), value_(value) {}

  ~JvmtiAllocation() {
    if (env_ != nullptr && value_ != nullptr) {
      static_cast<void>(
          env_->Deallocate(reinterpret_cast<unsigned char *>(value_)));
    }
  }

  JvmtiAllocation(const JvmtiAllocation &) = delete;
  JvmtiAllocation &operator=(const JvmtiAllocation &) = delete;

  [[nodiscard]] T **out() noexcept { return &value_; }
  [[nodiscard]] T *get() const noexcept { return value_; }
  [[nodiscard]] T &operator[](const std::size_t index) const noexcept {
    return value_[index];
  }

private:
  jvmtiEnv *env_;
  T *value_;
};

template <typename T> class JniLocalRef final {
public:
  JniLocalRef(JNIEnv *env, T value) noexcept : env_(env), value_(value) {}

  ~JniLocalRef() {
    if (env_ != nullptr && value_ != nullptr)
      env_->DeleteLocalRef(value_);
  }

  JniLocalRef(const JniLocalRef &) = delete;
  JniLocalRef &operator=(const JniLocalRef &) = delete;

  [[nodiscard]] T get() const noexcept { return value_; }

private:
  JNIEnv *env_;
  T value_;
};

} // namespace jankhunter::artti::art

#endif // JANKHUNTER_ARTTI_ART_JVMTI_RESOURCES_H_
