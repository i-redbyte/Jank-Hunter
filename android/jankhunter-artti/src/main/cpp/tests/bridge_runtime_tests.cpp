#include <iostream>

#include "bridge/artti_abi.h"
#include "bridge/bridge_runtime.h"

namespace {

using jankhunter::artti::bridge::ArtTiNativeConfigV1;
using jankhunter::artti::bridge::BridgeRuntime;

[[noreturn]] void Fail(const char* expression, const char* file, int line) {
  std::cerr << file << ':' << line << ": check failed: " << expression << '\n';
  std::abort();
}

#define JH_CHECK(expression) \
  do { \
    if (!(expression)) Fail(#expression, __FILE__, __LINE__); \
  } while (false)

void BridgeRuntimeStopAllowsReinitialize() {
  ArtTiNativeConfigV1 wire{};
  wire.config_hash = 42U;
  auto& bridge = BridgeRuntime::Instance();
  JH_CHECK(bridge.Initialize(wire).ok());
  JH_CHECK(bridge.callback_engine() != nullptr);
  JH_CHECK(bridge.Stop().ok());
  JH_CHECK(bridge.callback_engine() == nullptr);
  JH_CHECK(bridge.Initialize(wire).ok());
  JH_CHECK(bridge.callback_engine() != nullptr);
  JH_CHECK(bridge.Stop().ok());
  JH_CHECK(bridge.callback_engine() == nullptr);
}

}  // namespace

int main() {
  BridgeRuntimeStopAllowsReinitialize();
  std::cout << "jh_artti_bridge_tests: PASS\n";
  return 0;
}
