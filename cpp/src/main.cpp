#include <iostream>
#include <memory>
#include <string>
#include <chrono>
#include <cstdlib>

#include <grpcpp/grpcpp.h>
#include "extraction.grpc.pb.h"
#include "extractor.h"
#include "llm_labeler.h"

class ExtractionServiceImpl final : public extraction::ExtractionService::Service {
public:
    ExtractionServiceImpl(const std::string& redis_addr, LLMLabeler* labeler)
        : redis_addr_(redis_addr), labeler_(labeler) {}

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
            redis_addr_,
            labeler_
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
    LLMLabeler* labeler_;
};

int main() {
    std::string listen_addr = "0.0.0.0:50051";
    std::string redis_addr = "127.0.0.1";
    std::string llama_model_path = "/opt/models/llama-3.2-3b-instruct.Q4_K_M.gguf";
    int llama_context_size = 4096;
    int llama_max_tokens = 512;

    if (const char* env = std::getenv("EXTRACTOR_LISTEN_ADDR")) listen_addr = env;
    if (const char* env = std::getenv("REDIS_HOST")) redis_addr = env;
    if (const char* env = std::getenv("LLAMA_MODEL_PATH")) llama_model_path = env;
    if (const char* env = std::getenv("LLAMA_CONTEXT_SIZE")) llama_context_size = std::stoi(env);
    if (const char* env = std::getenv("LLAMA_MAX_TOKENS")) llama_max_tokens = std::stoi(env);

    // Initialize LLM (graceful if model not found).
    LLMLabeler labeler(llama_model_path, llama_context_size, llama_max_tokens);

    ExtractionServiceImpl service(redis_addr, labeler.is_loaded() ? &labeler : nullptr);
    grpc::ServerBuilder builder;
    builder.AddListeningPort(listen_addr, grpc::InsecureServerCredentials());
    builder.RegisterService(&service);

    auto server = builder.BuildAndStart();
    std::cout << "[INFO] extraction engine listening on " << listen_addr << std::endl;
    if (labeler.is_loaded()) {
        std::cout << "[INFO] LLM labeler active" << std::endl;
    } else {
        std::cout << "[WARN] LLM labeler not available — using raw text fallback" << std::endl;
    }

    server->Wait();
    return 0;
}
