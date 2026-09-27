cmake_minimum_required(VERSION 3.22.1)
project("localghost_llm")

# llama.cpp is our ONLY external native dependency. It is pinned to an exact, IMMUTABLE upstream
# commit (not a tag, which can be re-pointed) and fetched at configure time. After fetch, CMake
# verifies the checked-out commit matches the pin and fails the build on any mismatch. The pin
# here is the single source of truth: gradle reads it (the native build is skipped while it is
# empty), the build-env provenance records it, and BuildConfig.LLAMA_CPP_COMMIT carries it.
#
# PIN IT TO THE COMMIT THE BOX RUNS, so the phone and the box read the same GGUF the same way
# (Gemma 4 needs a llama.cpp from 2026; the box's already reads Gemma 4):
#     ssh box 'git -C /opt/localghost/llama.cpp rev-parse HEAD'
# and the tag it corresponds to, for humans (git -C /opt/localghost/llama.cpp describe --tags).
#
# Repo: https://github.com/ggml-org/llama.cpp
set(LLAMA_CPP_TAG    "" CACHE STRING "llama.cpp release tag (for humans)")
set(LLAMA_CPP_COMMIT "" CACHE STRING "llama.cpp pinned commit (verified)")

if(NOT LLAMA_CPP_COMMIT MATCHES "^[0-9a-f]+$")
    message(FATAL_ERROR "LLAMA_CPP_COMMIT is not set (gradle should not have configured this build).")
endif()

# A lean static llama.cpp inside our one shared library: no tools, no server, no curl (the phone
# never downloads anything through llama.cpp; the model comes from the box), no OpenMP.
set(BUILD_SHARED_LIBS    OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_TESTS    OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_TOOLS    OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_SERVER   OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_COMMON   OFF CACHE BOOL "" FORCE)
set(LLAMA_CURL           OFF CACHE BOOL "" FORCE)
set(GGML_OPENMP          OFF CACHE BOOL "" FORCE)
# The CPU the phone has, not the build machine's: -march from gradle's -PllamaArch (default
# armv8.2-a+dotprod+fp16, every arm64 phone on Android 15; +i8mm for Snapdragon 8 Gen 1 and later).
set(GGML_NATIVE          OFF CACHE BOOL "" FORCE)
set(LG_CPU_ARCH "armv8.2-a+dotprod+fp16" CACHE STRING "arm64 -march for the phone build")
set(GGML_CPU_ARM_ARCH    "${LG_CPU_ARCH}" CACHE STRING "" FORCE)

include(FetchContent)
# -DFETCHCONTENT_SOURCE_DIR_LLAMA_CPP=/path/to/llama.cpp (gradle -PllamaSrc=…) uses a local checkout
# instead of fetching; it is verified against the pin all the same when it is a git checkout.
FetchContent_Declare(
    llama_cpp
    GIT_REPOSITORY https://github.com/ggml-org/llama.cpp.git
    GIT_TAG        ${LLAMA_CPP_COMMIT}   # full commit SHA: immutable, content-addressed
)
FetchContent_MakeAvailable(llama_cpp)

# Verify the source is exactly the pinned commit: a tampered cache, a redirected fetch or the wrong
# local checkout fails the build loudly.
FetchContent_GetProperties(llama_cpp SOURCE_DIR LLAMA_SRC)
if(EXISTS "${LLAMA_SRC}/.git")
    execute_process(
        COMMAND git -C "${LLAMA_SRC}" rev-parse HEAD
        OUTPUT_VARIABLE LLAMA_ACTUAL_COMMIT
        OUTPUT_STRIP_TRAILING_WHITESPACE
    )
    if(NOT LLAMA_ACTUAL_COMMIT STREQUAL LLAMA_CPP_COMMIT)
        message(FATAL_ERROR
            "llama.cpp commit mismatch.\n"
            "  pinned:   ${LLAMA_CPP_COMMIT}\n"
            "  found:    ${LLAMA_ACTUAL_COMMIT}\n"
            "Refusing to build against unverified llama.cpp source.")
    endif()
    message(STATUS "llama.cpp verified at ${LLAMA_CPP_COMMIT} (tag ${LLAMA_CPP_TAG}), -march=${LG_CPU_ARCH}")
else()
    message(WARNING "llama.cpp source at ${LLAMA_SRC} is not a git checkout; it cannot be verified against ${LLAMA_CPP_COMMIT}.")
endif()

add_library(localghost_llm SHARED llama_jni.cpp)
target_compile_options(localghost_llm PRIVATE -O3)

find_library(log-lib log)
target_link_libraries(localghost_llm llama ${log-lib})
