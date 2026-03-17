FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    cmake build-essential pkg-config \
    libpoppler-cpp-dev \
    libtesseract-dev tesseract-ocr tesseract-ocr-eng \
    libopencv-dev \
    libhiredis-dev \
    libgrpc++-dev libprotobuf-dev protobuf-compiler-grpc \
    libcurl4-openssl-dev \
    libssl-dev \
    && rm -rf /var/lib/apt/lists/*

COPY proto/ /app/proto/
COPY cpp/ /app/cpp/

WORKDIR /app/cpp

RUN mkdir -p build && cd build && cmake .. && make -j$(nproc)

EXPOSE 50051

ENV EXTRACTOR_LISTEN_ADDR=0.0.0.0:50051
ENV REDIS_HOST=host.docker.internal

CMD ["/app/cpp/build/extractor"]
