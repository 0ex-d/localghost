cmake_minimum_required(VERSION 3.22.1)
project("localghost_llm")

# llama.cpp is our ONLY external native dependency, and the phone builds the SAME BYTES the box
# builds from: the llama.cpp source tarball on the LocalGhost mirror (set `llama`), pinned here by
# its SHA-256. No git, no GitHub at build time. CMake locates the file through the mirror's
# MANIFEST.txt (builds are pruned, so the path is looked up, not written down) and checks the
# download against the pin; a tarball copied from the box works the same way:
#     ./gradlew assembleRelease -PllamaTarball=/path/to/llama.cpp-v0.5.0-7fe450e.tar.gz
# (on the box it is /opt/localghost/llama.cpp.mirror-dl/<name>, verified there at setup).
#
# The pin is the single source of truth: gradle reads it (no native build while it is empty),
# the build-env provenance records it, BuildConfig.LLAMA_CPP_COMMIT carries it. To set or move it:
#     app/android/tools/pin_llama.sh          (reads the signed mirror manifest, writes the lines below)
set(LLAMA_CPP_TARBALL "llama.cpp-v0.5.0-7fe450e.tar.gz" CACHE STRING "llama.cpp source tarball on the mirror")
set(LLAMA_CPP_SHA256  "" CACHE STRING "its SHA-256 (the pin)")
set(LLAMA_CPP_TAG     "v0.5.0" CACHE STRING "for humans")
set(LLAMA_CPP_COMMIT  "7fe450e" CACHE STRING "for humans: the upstream commit the tarball was cut from")
set(LG_MIRROR "https://www.localghost.ai/mirror" CACHE STRING "the LocalGhost mirror")
set(LLAMA_CPP_TARBALL_PATH "" CACHE FILEPATH "a local copy of the tarball (gradle -PllamaTarball)")

if(NOT LLAMA_CPP_SHA256 MATCHES "^[0-9a-f]+$")
    message(FATAL_ERROR "LLAMA_CPP_SHA256 is not set (gradle should not have configured this build): app/android/tools/pin_llama.sh")
endif()

# where the tarball comes from: a local copy, or the mirror path the manifest gives for this hash
if(LLAMA_CPP_TARBALL_PATH)
    set(LLAMA_URL "file://${LLAMA_CPP_TARBALL_PATH}")
else()
    set(_manifest "${CMAKE_BINARY_DIR}/mirror-MANIFEST.txt")
    file(DOWNLOAD "${LG_MIRROR}/MANIFEST.txt" "${_manifest}" STATUS _st TLS_VERIFY ON TIMEOUT 60)
    list(GET _st 0 _code)
    if(NOT _code EQUAL 0)
        list(GET _st 1 _msg)
        message(FATAL_ERROR "could not read ${LG_MIRROR}/MANIFEST.txt (${_msg}); copy the tarball from the box and pass -PllamaTarball=…")
    endif()
    # the line for exactly this hash and name: the manifest only says WHERE, the pin says WHAT
    file(STRINGS "${_manifest}" _line REGEX "^${LLAMA_CPP_SHA256}  /[^/]+/llama/${LLAMA_CPP_TARBALL}$")
    if(NOT _line)
        message(FATAL_ERROR "the mirror's manifest has no ${LLAMA_CPP_TARBALL} with SHA-256 ${LLAMA_CPP_SHA256} (a newer llama.cpp was published: app/android/tools/pin_llama.sh moves the pin)")
    endif()
    string(REGEX REPLACE "^[0-9a-f]+  " "" _path "${_line}")
    set(LLAMA_URL "${LG_MIRROR}${_path}")
endif()
message(STATUS "llama.cpp ${LLAMA_CPP_TAG} (${LLAMA_CPP_COMMIT}) from ${LLAMA_URL}, SHA-256 ${LLAMA_CPP_SHA256}")

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
FetchContent_Declare(
    llama_cpp
    URL      "${LLAMA_URL}"
    URL_HASH SHA256=${LLAMA_CPP_SHA256}   # CMake refuses the source on any mismatch
    DOWNLOAD_EXTRACT_TIMESTAMP TRUE
)
FetchContent_MakeAvailable(llama_cpp)

add_library(localghost_llm SHARED llama_jni.cpp)
target_compile_options(localghost_llm PRIVATE -O3)

find_library(log-lib log)
target_link_libraries(localghost_llm llama ${log-lib})
