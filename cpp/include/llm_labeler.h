#pragma once

#include "extractor.h"
#include <string>
#include <vector>
#include <memory>

class LLMLabeler {
public:
    explicit LLMLabeler(const std::string& model_path, int context_size = 4096, int max_tokens = 512);
    ~LLMLabeler();

    // Takes raw extracted text + layout, returns JSON of labeled fields.
    std::string LabelFields(
        const std::string& raw_text,
        const std::vector<TextBlock>& layout,
        const std::string& file_type
    );

    bool is_loaded() const;

private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};
