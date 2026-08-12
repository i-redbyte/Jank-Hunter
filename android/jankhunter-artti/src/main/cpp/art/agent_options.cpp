#include "art/agent_options.h"

#include <cstdint>
#include <cstring>
#include <limits>
#include <string_view>

namespace jankhunter::artti::art {
namespace {

bool ParseUnsigned(std::string_view text, std::uint64_t* output) noexcept {
  if (output == nullptr || text.empty()) return false;
  std::uint32_t radix = 10U;
  std::size_t index = 0U;
  if (text.size() > 2U && text[0] == '0' && (text[1] == 'x' || text[1] == 'X')) {
    radix = 16U;
    index = 2U;
  }
  std::uint64_t value = 0U;
  for (; index < text.size(); ++index) {
    const char character = text[index];
    std::uint32_t digit = 0U;
    if (character >= '0' && character <= '9') {
      digit = static_cast<std::uint32_t>(character - '0');
    } else if (radix == 16U && character >= 'a' && character <= 'f') {
      digit = static_cast<std::uint32_t>(character - 'a') + 10U;
    } else if (radix == 16U && character >= 'A' && character <= 'F') {
      digit = static_cast<std::uint32_t>(character - 'A') + 10U;
    } else {
      return false;
    }
    if (digit >= radix || value > (std::numeric_limits<std::uint64_t>::max() - digit) / radix) {
      return false;
    }
    value = value * radix + digit;
  }
  *output = value;
  return true;
}

bool Assign(
    const std::string_view key,
    const std::uint64_t value,
    bridge::ArtTiNativeConfigV1* const config) noexcept {
  if (key == "v") return value == 1U;
  if (key == "profile" && value <= 4U) {
    config->profile = static_cast<std::uint32_t>(value);
  } else if (key == "transport" && value <= UINT32_MAX) {
    config->transport_capacity = static_cast<std::uint32_t>(value);
  } else if (key == "threads" && value <= UINT32_MAX) {
    config->max_tracked_threads = static_cast<std::uint32_t>(value);
  } else if (key == "contentions" && value <= UINT32_MAX) {
    config->max_open_contentions = static_cast<std::uint32_t>(value);
  } else if (key == "depth" && value <= UINT32_MAX) {
    config->max_stack_depth = static_cast<std::uint32_t>(value);
  } else if (key == "stackdefs" && value <= UINT32_MAX) {
    config->max_stack_definitions = static_cast<std::uint32_t>(value);
  } else if (key == "methoddefs" && value <= UINT32_MAX) {
    config->max_method_definitions = static_cast<std::uint32_t>(value);
  } else if (key == "batch" && value <= UINT32_MAX) {
    config->drain_batch_size = static_cast<std::uint32_t>(value);
  } else if (key == "mincontentionns") {
    config->min_contention_duration_ns = value;
  } else if (key == "hash") {
    config->config_hash = value;
  } else if (key == "cap") {
    config->requested_capabilities = value;
  } else {
    return false;
  }
  return true;
}

}  // namespace

Status ParseAgentOptions(
    const char* const options, bridge::ArtTiNativeConfigV1* const config) noexcept {
  if (config == nullptr) return Status::Error(StatusCode::kInvalidArgument);
  if (options == nullptr || options[0] == '\0') return Status::Ok();
  const auto length = strnlen(options, kMaxAgentOptionsLength + 1U);
  if (length == 0U || length > kMaxAgentOptionsLength) {
    return Status::Error(StatusCode::kInvalidArgument);
  }
  std::string_view remaining(options, length);
  std::uint32_t field_count = 0U;
  while (!remaining.empty()) {
    if (++field_count > 32U) return Status::Error(StatusCode::kInvalidArgument);
    const auto separator = remaining.find(';');
    const auto field = remaining.substr(0U, separator);
    remaining = separator == std::string_view::npos
        ? std::string_view{}
        : remaining.substr(separator + 1U);
    const auto equals = field.find('=');
    if (equals == std::string_view::npos || equals == 0U || equals + 1U >= field.size()) {
      return Status::Error(StatusCode::kInvalidArgument);
    }
    std::uint64_t value = 0U;
    if (!ParseUnsigned(field.substr(equals + 1U), &value) ||
        !Assign(field.substr(0U, equals), value, config)) {
      return Status::Error(StatusCode::kInvalidArgument);
    }
  }
  return Status::Ok();
}

}  // namespace jankhunter::artti::art
