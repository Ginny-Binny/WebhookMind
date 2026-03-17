#include <iostream>
#include <memory>
#include <string>
#include <chrono>
#include <cstdlib>

#include <grpcpp/grpcpp.h>
#include "extraction.grpc.pb.h"
#include "extractor.h"

class ExtractionServiceImpl final : public extraction::ExtractionService::Service {
public:
    explicit ExtractionServiceImpl(const std::string& redis_addr)
        : redis_addr_(redis_addr) {}

    grpc::Status Extract(
        grpc::ServerContext* context,
        const extraction::ExtractionRequest* request,
        extraction::ExtractionResponse* response
    ) override {
        auto start = std::chrono::steady_clock::now();

        std::cout << "[INFO] extraction request: event_id=" << request->event_id()
                  << " file_type=" << request->file_type()
                  << " source_id=" << request->source_id() << std::endl;

        auto result = extract_document(
            request->file_path(),
            request->file_type(),
            request->source_id(),
            request->presigned_url(),
            redis_addr_
        );

        auto end = std::chrono::steady_clock::now();
        auto duration_ms = std::chrono::duration_cast<std::chrono::milliseconds>(end - start).count();

        response->set_success(result.success);
        response->set_error_message(result.error_message);
        response->set_extracted_json(result.extracted_json);
        response->set_template_id(result.template_id);
        response->set_cache_hit(result.cache_hit);
        response->set_duration_ms(duration_ms);

        if (result.success) {
            std::cout << "[INFO] extraction succeeded: event_id=" << request->event_id()
                      << " template_id=" << result.template_id
                      << " cache_hit=" << (result.cache_hit ? "true" : "false")
                      << " duration_ms=" << duration_ms << std::endl;
        } else {
            std::cerr << "[ERROR] extraction failed: event_id=" << request->event_id()
                      << " error=" << result.error_message << std::endl;
        }

        return grpc::Status::OK;
    }

private:
    std::string redis_addr_;
};

int main() {
    std::string listen_addr = "0.0.0.0:50051";
    std::string redis_addr = "127.0.0.1";

    if (const char* env = std::getenv("EXTRACTOR_LISTEN_ADDR")) {
        listen_addr = env;
    }
    if (const char* env = std::getenv("REDIS_HOST")) {
        redis_addr = env;
    }

    ExtractionServiceImpl service(redis_addr);
    grpc::ServerBuilder builder;
    builder.AddListeningPort(listen_addr, grpc::InsecureServerCredentials());
    builder.RegisterService(&service);

    auto server = builder.BuildAndStart();
    std::cout << "[INFO] extraction engine listening on " << listen_addr << std::endl;

    server->Wait();
    return 0;
}
