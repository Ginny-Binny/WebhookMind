#pragma once

#include <string>
#include <vector>
#include <cstdint>

struct TranscriptionSegment {
    int64_t start_ms;
    int64_t end_ms;
    std::string text;
};

struct TranscriptionResult {
    std::string full_text;
    std::vector<TranscriptionSegment> segments;
    std::string language;
    float confidence;
    bool success;
    std::string error_message;
};

TranscriptionResult transcribe_audio(const std::string& file_path);
