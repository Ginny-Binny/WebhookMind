#include "extractor.h"
#include "pdf_handler.h"
#include "ocr_handler.h"
#include "audio_handler.h"
#include "fingerprinter.h"
#include "template_cache.h"
#include "llm_labeler.h"

#include <curl/curl.h>
#include <fstream>
#include <iostream>
#include <sstream>
#include <cstdio>

static size_t write_callback(void* contents, size_t size, size_t nmemb, void* userp) {
    auto* data = static_cast<std::string*>(userp);
    data->append(static_cast<char*>(contents), size * nmemb);
    return size * nmemb;
}

static bool download_file(const std::string& url, const std::string& output_path) {
    CURL* curl = curl_easy_init();
    if (!curl) {
        std::cerr << "[ERROR] curl_easy_init failed" << std::endl;
        return false;
    }

    std::cerr << "[DEBUG] downloading from: " << url.substr(0, 120) << "..." << std::endl;

    std::string buffer;
    curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, write_callback);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &buffer);
    curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 1L);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 120L);

    CURLcode res = curl_easy_perform(curl);
    long http_code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &http_code);
    curl_easy_cleanup(curl);

    if (res != CURLE_OK) {
        std::cerr << "[ERROR] curl failed: " << curl_easy_strerror(res) << std::endl;
        return false;
    }

    if (http_code != 200) {
        std::cerr << "[ERROR] download HTTP status: " << http_code << " body: " << buffer.substr(0, 200) << std::endl;
        return false;
    }

    std::cerr << "[DEBUG] downloaded " << buffer.size() << " bytes" << std::endl;

    std::ofstream ofs(output_path, std::ios::binary);
    if (!ofs) {
        std::cerr << "[ERROR] failed to open output file: " << output_path << std::endl;
        return false;
    }
    ofs.write(buffer.data(), buffer.size());
    return true;
}

// Build a simple JSON string from text blocks (raw text extraction).
static std::string blocks_to_json(const std::vector<TextBlock>& blocks) {
    std::ostringstream oss;
    oss << "{\"raw_text\":\"";
    for (const auto& block : blocks) {
        for (char c : block.text) {
            if (c == '"') oss << "\\\"";
            else if (c == '\\') oss << "\\\\";
            else if (c == '\n') oss << "\\n";
            else if (c == '\r') oss << "\\r";
            else if (c == '\t') oss << "\\t";
            else oss << c;
        }
        oss << " ";
    }
    oss << "\"}";
    return oss.str();
}

// Helper to build raw text from blocks.
static std::string blocks_to_raw_text(const std::vector<TextBlock>& blocks) {
    std::string text;
    for (const auto& b : blocks) {
        text += b.text + " ";
    }
    return text;
}

ExtractionResult extract_document(
    const std::string& file_path,
    const std::string& file_type,
    const std::string& source_id,
    const std::string& presigned_url,
    const std::string& redis_addr,
    LLMLabeler* labeler
) {
    ExtractionResult result;
    result.success = false;
    result.cache_hit = false;

    // Download file from presigned URL to a temp path.
    std::string tmp_path = "/tmp/webhookmind_" + file_path;
    // Create parent directories.
    std::string mkdir_cmd = "mkdir -p $(dirname " + tmp_path + ")";
    std::system(mkdir_cmd.c_str());

    if (!download_file(presigned_url, tmp_path)) {
        result.error_message = "failed to download file from presigned URL";
        return result;
    }

    // Extract text blocks based on file type.
    std::vector<TextBlock> blocks;
    double page_width = 612.0;  // default letter size
    double page_height = 792.0;

    if (file_type == "pdf") {
        blocks = extract_pdf(tmp_path);
    } else if (file_type == "image") {
        blocks = extract_image(tmp_path);
    } else if (file_type == "audio") {
        auto transcription = transcribe_audio(tmp_path);
        if (!transcription.success) {
            result.error_message = transcription.error_message;
            std::remove(tmp_path.c_str());
            return result;
        }
        // For audio, use LLM to extract entities from transcription text.
        if (labeler && labeler->is_loaded() && !transcription.full_text.empty()) {
            std::string labeled = labeler->LabelFields(transcription.full_text, {}, "audio");
            if (!labeled.empty()) {
                result.extracted_json = labeled;
            } else {
                result.extracted_json = "{\"transcription\":\"" + transcription.full_text + "\"}";
            }
        } else {
            result.extracted_json = "{\"transcription\":\"" + transcription.full_text + "\"}";
        }
        result.success = true;
        std::remove(tmp_path.c_str());
        return result;
    } else {
        result.error_message = "unsupported file type: " + file_type;
        std::remove(tmp_path.c_str());
        return result;
    }

    if (blocks.empty()) {
        result.error_message = "no text extracted from document";
        result.extracted_json = "{\"raw_text\":\"\"}";
        result.success = true; // empty but not an error
        std::remove(tmp_path.c_str());
        return result;
    }

    // Compute fingerprint.
    std::string template_id = compute_fingerprint(blocks, page_width, page_height);
    result.template_id = template_id;

    // Check template cache.
    TemplateCache cache(redis_addr);
    if (cache.is_connected()) {
        std::string cached = cache.get(template_id);
        if (!cached.empty()) {
            result.success = true;
            result.cache_hit = true;
            result.extracted_json = cached;
            std::remove(tmp_path.c_str());
            return result;
        }
    }

    // Cache miss: use LLM for structured field labeling if available.
    if (labeler && labeler->is_loaded()) {
        std::string raw = blocks_to_raw_text(blocks);
        std::string labeled = labeler->LabelFields(raw, blocks, file_type);
        if (!labeled.empty()) {
            result.extracted_json = labeled;
        } else {
            result.extracted_json = blocks_to_json(blocks); // fallback
        }
    } else {
        result.extracted_json = blocks_to_json(blocks); // fallback
    }
    result.success = true;

    // Store in cache for next time.
    if (cache.is_connected()) {
        cache.set(template_id, result.extracted_json);
    }

    std::remove(tmp_path.c_str());
    return result;
}
