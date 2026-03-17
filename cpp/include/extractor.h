#pragma once

#include <string>
#include <vector>

class LLMLabeler; // forward declaration

struct TextBlock {
    std::string text;
    double x, y, w, h;
};

struct ExtractionResult {
    bool success;
    std::string error_message;
    std::string extracted_json;
    std::string template_id;
    bool cache_hit;
    std::vector<TextBlock> blocks;
};

ExtractionResult extract_document(
    const std::string& file_path,
    const std::string& file_type,
    const std::string& source_id,
    const std::string& presigned_url,
    const std::string& redis_addr,
    LLMLabeler* labeler = nullptr
);
