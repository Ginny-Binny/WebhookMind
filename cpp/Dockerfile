FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    cmake build-essential pkg-config git \
    libpoppler-cpp-dev \
    libtesseract-dev tesseract-ocr tesseract-ocr-eng \
    libopencv-dev \
    libhiredis-dev \
    libgrpc++-dev libprotobuf-dev protobuf-compiler-grpc \
    libcurl4-openssl-dev \
    libssl-dev \
    ffmpeg \
    && rm -rf /var/lib/apt/lists/*

# Clone llama.cpp (pinned tag) and whisper.cpp into vendor
RUN mkdir -p /app/cpp/vendor && \
    git clone --branch b5200 --depth 1 https://github.com/ggerganov/llama.cpp /app/cpp/vendor/llama.cpp && \
    git clone --branch v1.7.4 --depth 1 https://github.com/ggerganov/whisper.cpp /app/cpp/vendor/whisper.cpp

# Pre-build whisper.cpp as a standalone static library to avoid ggml symbol conflicts with llama.cpp
RUN cd /app/cpp/vendor/whisper.cpp && \
    mkdir -p build && cd build && \
    cmake .. -DBUILD_SHARED_LIBS=OFF -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF && \
    make -j$(nproc) whisper

COPY proto/ /app/proto/
COPY cpp/CMakeLists.txt /app/cpp/CMakeLists.txt
COPY cpp/include/ /app/cpp/include/
COPY cpp/src/ /app/cpp/src/
COPY cpp/proto/ /app/cpp/proto/

WORKDIR /app/cpp

RUN mkdir -p build && cd build && cmake .. && make -j$(nproc)

EXPOSE 50051

VOLUME /opt/models

ENV EXTRACTOR_LISTEN_ADDR=0.0.0.0:50051
ENV REDIS_HOST=host.docker.internal
ENV LLAMA_MODEL_PATH=/opt/models/llama-3.2-3b-instruct.Q4_K_M.gguf
ENV LLAMA_CONTEXT_SIZE=4096
ENV LLAMA_MAX_TOKENS=512
ENV WHISPER_MODEL_PATH=/opt/models/whisper-base.en.bin

CMD ["/app/cpp/build/extractor"]
