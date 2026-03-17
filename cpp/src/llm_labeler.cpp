#include "llm_labeler.h"

#include <llama.h>
#include <iostream>
#include <sstream>
#include <fstream>
#include <algorithm>
#include <vector>

struct LLMLabeler::Impl {
    llama_model* model = nullptr;
    llama_context* ctx = nullptr;
    const llama_vocab* vocab = nullptr;
    int context_size;
    int max_tokens;
    bool loaded = false;
};

LLMLabeler::LLMLabeler(const std::string& model_path, int context_size, int max_tokens)
    : impl_(std::make_unique<Impl>()) {
    impl_->context_size = context_size;
    impl_->max_tokens = max_tokens;

    std::ifstream test(model_path);
    if (!test.good()) {
        std::cerr << "[WARN] LLM model not found at: " << model_path
                  << " — falling back to raw text extraction" << std::endl;
        return;
    }
    test.close();

    llama_backend_init();

    auto model_params = llama_model_default_params();
    impl_->model = llama_model_load_from_file(model_path.c_str(), model_params);
    if (!impl_->model) {
        std::cerr << "[ERROR] failed to load LLM model from: " << model_path << std::endl;
        return;
    }

    impl_->vocab = llama_model_get_vocab(impl_->model);

    auto ctx_params = llama_context_default_params();
    ctx_params.n_ctx = context_size;
    ctx_params.n_batch = 512;
    impl_->ctx = llama_init_from_model(impl_->model, ctx_params);
    if (!impl_->ctx) {
        std::cerr << "[ERROR] failed to create LLM context" << std::endl;
        llama_model_free(impl_->model);
        impl_->model = nullptr;
        return;
    }

    impl_->loaded = true;
    std::cout << "[INFO] LLM model loaded successfully: " << model_path << std::endl;
}

LLMLabeler::~LLMLabeler() {
    if (impl_->ctx) llama_free(impl_->ctx);
    if (impl_->model) llama_model_free(impl_->model);
    llama_backend_free();
}

bool LLMLabeler::is_loaded() const {
    return impl_->loaded;
}

std::string LLMLabeler::LabelFields(
    const std::string& raw_text,
    const std::vector<TextBlock>& layout,
    const std::string& file_type
) {
    if (!impl_->loaded) return "";

    // Build prompt.
    std::ostringstream prompt;
    prompt << "You are a structured data extractor. Given text extracted from a document, "
           << "identify and extract key fields as JSON. Return ONLY valid JSON, no explanation.\n\n"
           << "Common fields to look for: invoice_number, amount, currency, vendor, customer, "
           << "date, due_date, po_number, tax_amount, total_amount, description.\n\n"
           << "For other document types, infer appropriate field names from context.\n"
           << "Always return snake_case field names. Amounts should be numeric, not strings.\n\n"
           << "Document type: " << file_type << "\n\n"
           << "Extract fields from this document text:\n";

    int max_text_chars = (impl_->context_size - 300) * 3;
    if ((int)raw_text.length() > max_text_chars) {
        prompt << raw_text.substr(0, max_text_chars) << "\n\n[text truncated]\n\n";
    } else {
        prompt << raw_text << "\n\n";
    }

    prompt << "Return format: {\"field_name\": value, ...}\n";

    std::string prompt_str = prompt.str();

    // Tokenize.
    int n_prompt_tokens = prompt_str.length() / 3 + 64;
    std::vector<llama_token> tokens(n_prompt_tokens);
    int n_tokens = llama_tokenize(impl_->vocab, prompt_str.c_str(), prompt_str.length(),
                                   tokens.data(), tokens.size(), true, true);
    if (n_tokens < 0) {
        tokens.resize(-n_tokens);
        n_tokens = llama_tokenize(impl_->vocab, prompt_str.c_str(), prompt_str.length(),
                                   tokens.data(), tokens.size(), true, true);
    }
    if (n_tokens < 0) {
        std::cerr << "[ERROR] failed to tokenize prompt" << std::endl;
        return "";
    }
    tokens.resize(n_tokens);

    // Clear KV cache for fresh generation.
    llama_kv_cache_clear(impl_->ctx);

    // Process prompt tokens one batch at a time.
    int batch_size = 512;
    for (int i = 0; i < n_tokens; i += batch_size) {
        int n_batch = std::min(batch_size, n_tokens - i);
        if (llama_decode(impl_->ctx,
                llama_batch_get_one(tokens.data() + i, n_batch)) != 0) {
            std::cerr << "[ERROR] llama_decode failed for prompt batch" << std::endl;
            return "";
        }
    }

    // Generate tokens.
    std::string output;
    int n_cur = n_tokens;

    for (int i = 0; i < impl_->max_tokens; i++) {
        auto logits = llama_get_logits_ith(impl_->ctx, -1);

        // Greedy sampling.
        llama_token new_token = 0;
        float max_logit = logits[0];
        int vocab_size = llama_vocab_n_tokens(impl_->vocab);
        for (int j = 1; j < vocab_size; j++) {
            if (logits[j] > max_logit) {
                max_logit = logits[j];
                new_token = j;
            }
        }

        if (llama_vocab_is_eog(impl_->vocab, new_token)) break;

        char buf[256];
        int n = llama_token_to_piece(impl_->vocab, new_token, buf, sizeof(buf), 0, true);
        if (n > 0) {
            output.append(buf, n);
        }

        // Decode next token.
        if (llama_decode(impl_->ctx,
                llama_batch_get_one(&new_token, 1)) != 0) {
            break;
        }
        n_cur++;
    }

    // Extract JSON from output.
    auto json_start = output.find('{');
    auto json_end = output.rfind('}');
    if (json_start != std::string::npos && json_end != std::string::npos && json_end > json_start) {
        return output.substr(json_start, json_end - json_start + 1);
    }

    std::cerr << "[WARN] LLM did not produce valid JSON: " << output.substr(0, 200) << std::endl;
    return "";
}
