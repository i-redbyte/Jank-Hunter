cmake_minimum_required(VERSION 3.22.1)
project(jankhunter_artti LANGUAGES CXX)

option(JH_BUILD_HOST_TESTS "Build host native tests and benchmarks" OFF)
option(JH_ENABLE_SANITIZERS "Enable address and undefined behavior sanitizers" OFF)
option(JH_ENABLE_TSAN "Enable thread sanitizer" OFF)

if(JH_ENABLE_SANITIZERS AND JH_ENABLE_TSAN)
    message(FATAL_ERROR "JH_ENABLE_SANITIZERS and JH_ENABLE_TSAN are mutually exclusive")
endif()

add_library(jh_artti_core STATIC
    art/agent_options.cpp
    art/capability_negotiator.cpp
    core/engine.cpp
    core/interval_tracker.cpp
    core/thread_registry.cpp
    protocol/batch_encoder.cpp
)
target_include_directories(jh_artti_core PUBLIC ${CMAKE_CURRENT_SOURCE_DIR})
target_compile_features(jh_artti_core PUBLIC cxx_std_20)
set_target_properties(jh_artti_core PROPERTIES
    CXX_EXTENSIONS OFF
    POSITION_INDEPENDENT_CODE ON
)

if(CMAKE_CXX_COMPILER_ID MATCHES "Clang|GNU")
    target_compile_options(jh_artti_core PRIVATE
        -Wall
        -Wextra
        -Wconversion
        -Werror
        -fno-exceptions
        -fno-rtti
        -fvisibility=hidden
    )
endif()

if(JH_ENABLE_SANITIZERS AND CMAKE_CXX_COMPILER_ID MATCHES "Clang|GNU")
    target_compile_options(jh_artti_core PUBLIC -fsanitize=address,undefined -fno-omit-frame-pointer)
    target_link_options(jh_artti_core PUBLIC -fsanitize=address,undefined)
endif()

if(JH_ENABLE_TSAN AND CMAKE_CXX_COMPILER_ID MATCHES "Clang|GNU")
    target_compile_options(jh_artti_core PUBLIC -fsanitize=thread -fno-omit-frame-pointer)
    target_link_options(jh_artti_core PUBLIC -fsanitize=thread)
endif()

if(ANDROID)
    add_library(jankhunter_artti SHARED
        bootstrap.cpp
        art/art_jvmti_adapter.cpp
        bridge/bridge_runtime.cpp
        bridge/jni_bridge.cpp
    )
    target_link_libraries(jankhunter_artti PRIVATE jh_artti_core)
    target_include_directories(jankhunter_artti PRIVATE
        ${CMAKE_CURRENT_SOURCE_DIR}/third_party/jvmti
    )
    target_compile_features(jankhunter_artti PRIVATE cxx_std_20)
    if(CMAKE_CXX_COMPILER_ID MATCHES "Clang|GNU")
        target_compile_options(jankhunter_artti PRIVATE
            -Wall
            -Wextra
            -Wconversion
            -Werror
            -fno-exceptions
            -fno-rtti
            -fvisibility=hidden
        )
        target_link_options(jankhunter_artti PRIVATE -Wl,--exclude-libs,ALL)
    endif()
endif()

if(JH_BUILD_HOST_TESTS AND NOT ANDROID)
    find_package(Threads REQUIRED)

    add_executable(jh_artti_core_tests tests/core_tests.cpp)
    target_link_libraries(jh_artti_core_tests PRIVATE jh_artti_core Threads::Threads)
    target_compile_features(jh_artti_core_tests PRIVATE cxx_std_20)

    add_executable(jh_artti_core_bench benchmarks/core_bench.cpp)
    target_link_libraries(jh_artti_core_bench PRIVATE jh_artti_core Threads::Threads)
    target_compile_features(jh_artti_core_bench PRIVATE cxx_std_20)

    enable_testing()
    add_test(NAME jh_artti_core_tests COMMAND jh_artti_core_tests)
endif()
