cmake_minimum_required(VERSION 3.16)
project(webhookmind_extractor)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

find_package(PkgConfig REQUIRED)

# Poppler
pkg_check_modules(POPPLER REQUIRED poppler-cpp)

# Tesseract + Leptonica
pkg_check_modules(TESSERACT REQUIRED tesseract)
pkg_check_modules(LEPTONICA REQUIRED lept)

# OpenCV
find_package(OpenCV REQUIRED)

# Hiredis
pkg_check_modules(HIREDIS REQUIRED hiredis)

# gRPC + Protobuf
find_package(Protobuf REQUIRED)
find_package(gRPC CONFIG QUIET)

if(NOT gRPC_FOUND)
    # Fallback: find grpc via pkg-config
    pkg_check_modules(GRPC REQUIRED grpc++ grpc)
    set(GRPC_LIBRARIES ${GRPC_LIBRARIES})
else()
    set(GRPC_LIBRARIES gRPC::grpc++)
endif()

# CURL for downloading from presigned URLs
find_package(CURL REQUIRED)

# OpenSSL for SHA-256
find_package(OpenSSL REQUIRED)

# Llama.cpp (if vendor directory exists)
if(EXISTS "${CMAKE_SOURCE_DIR}/vendor/llama.cpp/CMakeLists.txt")
    set(LLAMA_BUILD_TESTS OFF CACHE BOOL "" FORCE)
    set(LLAMA_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
    set(LLAMA_BUILD_SERVER OFF CACHE BOOL "" FORCE)
    add_subdirectory(vendor/llama.cpp)
    set(LLAMA_AVAILABLE TRUE)
else()
    set(LLAMA_AVAILABLE FALSE)
    message(WARNING "llama.cpp not found in vendor/ — LLM labeling disabled")
endif()

# Generate protobuf/gRPC code
find_program(PROTOC protoc REQUIRED)
find_program(GRPC_CPP_PLUGIN grpc_cpp_plugin REQUIRED)

set(PROTO_SRC_DIR "${CMAKE_SOURCE_DIR}/proto")
set(PROTO_GEN_DIR "${CMAKE_BINARY_DIR}/generated")
file(MAKE_DIRECTORY ${PROTO_GEN_DIR})

set(PROTO_FILE "${PROTO_SRC_DIR}/extraction.proto")
set(PROTO_SRCS "${PROTO_GEN_DIR}/extraction.pb.cc")
set(PROTO_HDRS "${PROTO_GEN_DIR}/extraction.pb.h")
set(GRPC_SRCS "${PROTO_GEN_DIR}/extraction.grpc.pb.cc")
set(GRPC_HDRS "${PROTO_GEN_DIR}/extraction.grpc.pb.h")

add_custom_command(
    OUTPUT ${PROTO_SRCS} ${PROTO_HDRS} ${GRPC_SRCS} ${GRPC_HDRS}
    COMMAND ${PROTOC}
        --cpp_out=${PROTO_GEN_DIR}
        --grpc_out=${PROTO_GEN_DIR}
        --plugin=protoc-gen-grpc=${GRPC_CPP_PLUGIN}
        -I${PROTO_SRC_DIR}
        ${PROTO_FILE}
    DEPENDS ${PROTO_FILE}
    COMMENT "Generating protobuf/gRPC C++ code"
)

add_executable(extractor
    src/main.cpp
    src/extractor.cpp
    src/pdf_handler.cpp
    src/ocr_handler.cpp
    src/fingerprinter.cpp
    src/template_cache.cpp
    src/llm_labeler.cpp
    src/audio_handler.cpp
    ${PROTO_SRCS}
    ${GRPC_SRCS}
)

target_include_directories(extractor PRIVATE
    ${CMAKE_SOURCE_DIR}/include
    ${PROTO_GEN_DIR}
    ${POPPLER_INCLUDE_DIRS}
    ${TESSERACT_INCLUDE_DIRS}
    ${OpenCV_INCLUDE_DIRS}
    ${HIREDIS_INCLUDE_DIRS}
    ${Protobuf_INCLUDE_DIRS}
    ${CURL_INCLUDE_DIRS}
    ${CMAKE_SOURCE_DIR}/vendor/whisper.cpp/include
)

target_link_libraries(extractor
    ${POPPLER_LIBRARIES}
    ${TESSERACT_LIBRARIES}
    ${LEPTONICA_LIBRARIES}
    ${OpenCV_LIBS}
    ${HIREDIS_LIBRARIES}
    ${GRPC_LIBRARIES}
    ${Protobuf_LIBRARIES}
    ${CURL_LIBRARIES}
    OpenSSL::Crypto
)

# Whisper.cpp — link pre-built static library (built separately to avoid ggml conflicts with llama.cpp)
if(EXISTS "${CMAKE_SOURCE_DIR}/vendor/whisper.cpp/build/src/libwhisper.a")
    add_library(whisper STATIC IMPORTED)
    set_target_properties(whisper PROPERTIES
        IMPORTED_LOCATION "${CMAKE_SOURCE_DIR}/vendor/whisper.cpp/build/src/libwhisper.a"
    )
    # Also need whisper's ggml libs
    file(GLOB WHISPER_GGML_LIBS "${CMAKE_SOURCE_DIR}/vendor/whisper.cpp/build/ggml/src/libggml*.a")
    set(WHISPER_AVAILABLE TRUE)
else()
    set(WHISPER_AVAILABLE FALSE)
    message(WARNING "whisper.cpp not pre-built — audio transcription disabled")
endif()

if(LLAMA_AVAILABLE)
    target_link_libraries(extractor llama)
    target_compile_definitions(extractor PRIVATE LLAMA_AVAILABLE)
endif()

if(WHISPER_AVAILABLE)
    target_link_libraries(extractor whisper ${WHISPER_GGML_LIBS})
    target_compile_definitions(extractor PRIVATE WHISPER_AVAILABLE)
endif()
