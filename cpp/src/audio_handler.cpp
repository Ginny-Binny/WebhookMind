#include "audio_handler.h"

#include <iostream>
#include <cstdlib>
#include <cstdio>
#include <fstream>
#include <sstream>

// Whisper.cpp integration — conditionally compiled
#ifdef WHISPER_AVAILABLE
#include <whisper.h>

static struct whisper_context* whisper_ctx = nullptr;
static bool whisper_initialized = false;

static bool init_whisper() {
    if (whisper_initialized) return whisper_ctx != nullptr;
    whisper_initialized = true;

    const char* model_path = std::getenv("WHISPER_MODEL_PATH");
    if (!model_path) {
        model_path = "/opt/models/whisper-base.en.bin";
    }

    std::ifstream test(model_path);
    if (!test.good()) {
        std::cerr << "[WARN] Whisper model not found at: " << model_path << std::endl;
        return false;
    }
    test.close();

    struct whisper_context_params params = whisper_context_default_params();
    whisper_ctx = whisper_init_from_file_with_params(model_path, params);
    if (!whisper_ctx) {
        std::cerr << "[ERROR] failed to load Whisper model" << std::endl;
        return false;
    }

    std::cout << "[INFO] Whisper model loaded: " << model_path << std::endl;
    return true;
}

TranscriptionResult transcribe_audio(const std::string& file_path) {
    TranscriptionResult result;
    result.success = false;
    result.confidence = 0.0f;

    if (!init_whisper() || !whisper_ctx) {
        result.error_message = "whisper model not available";
        return result;
    }

    // Convert to WAV 16kHz mono if needed.
    std::string wav_path = file_path + ".wav";
    std::string cmd = "ffmpeg -y -i \"" + file_path + "\" -ar 16000 -ac 1 -f wav \"" + wav_path + "\" 2>/dev/null";
    int ret = std::system(cmd.c_str());
    if (ret != 0) {
        // Maybe it's already a WAV.
        wav_path = file_path;
    }

    // Read WAV file — whisper expects raw PCM float32 samples at 16kHz.
    // For simplicity, use whisper's built-in WAV reader.
    std::vector<float> pcm_data;

    // Read WAV header and data.
    std::ifstream wav(wav_path, std::ios::binary);
    if (!wav) {
        result.error_message = "failed to open audio file";
        return result;
    }

    // Skip WAV header (44 bytes) and read 16-bit PCM data.
    wav.seekg(44);
    std::vector<int16_t> samples;
    int16_t sample;
    while (wav.read(reinterpret_cast<char*>(&sample), sizeof(sample))) {
        samples.push_back(sample);
    }

    if (samples.empty()) {
        result.error_message = "no audio data found";
        return result;
    }

    // Convert to float32.
    pcm_data.resize(samples.size());
    for (size_t i = 0; i < samples.size(); i++) {
        pcm_data[i] = static_cast<float>(samples[i]) / 32768.0f;
    }

    // Run Whisper inference.
    struct whisper_full_params wparams = whisper_full_default_params(WHISPER_SAMPLING_GREEDY);
    wparams.print_realtime = false;
    wparams.print_progress = false;
    wparams.print_timestamps = false;
    wparams.single_segment = false;

    if (whisper_full(whisper_ctx, wparams, pcm_data.data(), pcm_data.size()) != 0) {
        result.error_message = "whisper inference failed";
        return result;
    }

    // Collect segments.
    int n_segments = whisper_full_n_segments(whisper_ctx);
    std::ostringstream full_text;

    for (int i = 0; i < n_segments; i++) {
        const char* text = whisper_full_get_segment_text(whisper_ctx, i);
        int64_t t0 = whisper_full_get_segment_t0(whisper_ctx, i) * 10; // centiseconds to ms
        int64_t t1 = whisper_full_get_segment_t1(whisper_ctx, i) * 10;

        TranscriptionSegment seg;
        seg.start_ms = t0;
        seg.end_ms = t1;
        seg.text = text ? text : "";
        result.segments.push_back(seg);

        full_text << seg.text << " ";
    }

    result.full_text = full_text.str();
    result.language = "en"; // base.en model
    result.success = true;

    // Clean up temp WAV.
    if (wav_path != file_path) {
        std::remove(wav_path.c_str());
    }

    std::cout << "[INFO] audio transcribed: " << n_segments << " segments, "
              << pcm_data.size() / 16000 << "s" << std::endl;

    return result;
}

#else
// Stub when Whisper is not available.
TranscriptionResult transcribe_audio(const std::string& file_path) {
    TranscriptionResult result;
    result.success = false;
    result.error_message = "whisper not compiled — audio transcription unavailable";
    return result;
}
#endif
