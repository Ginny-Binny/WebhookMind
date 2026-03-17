#include "ocr_handler.h"

#include <opencv2/opencv.hpp>
#include <tesseract/baseapi.h>
#include <leptonica/allheaders.h>
#include <memory>
#include <mutex>
#include <iostream>

static std::mutex tess_mutex;
static std::unique_ptr<tesseract::TessBaseAPI> tess_api;
static bool tess_initialized = false;

static bool init_tesseract() {
    if (tess_initialized) return true;

    tess_api = std::make_unique<tesseract::TessBaseAPI>();
    if (tess_api->Init(nullptr, "eng")) {
        std::cerr << "[ERROR] failed to initialize Tesseract" << std::endl;
        tess_api.reset();
        return false;
    }
    tess_initialized = true;
    return true;
}

std::vector<TextBlock> extract_image(const std::string& file_path) {
    std::vector<TextBlock> blocks;

    // Load image with OpenCV.
    cv::Mat img = cv::imread(file_path);
    if (img.empty()) {
        std::cerr << "[ERROR] failed to load image: " << file_path << std::endl;
        return blocks;
    }

    // Preprocess: convert to grayscale.
    cv::Mat gray;
    cv::cvtColor(img, gray, cv::COLOR_BGR2GRAY);

    // Apply Otsu's thresholding.
    cv::Mat binary;
    cv::threshold(gray, binary, 0, 255, cv::THRESH_BINARY | cv::THRESH_OTSU);

    // OCR with Tesseract (mutex-protected, not thread-safe).
    std::lock_guard<std::mutex> lock(tess_mutex);
    if (!init_tesseract()) {
        return blocks;
    }

    tess_api->SetImage(binary.data, binary.cols, binary.rows, 1, binary.step);

    // Get word-level bounding boxes.
    Boxa* word_boxes = tess_api->GetComponentImages(tesseract::RIL_WORD, true, nullptr, nullptr);
    if (word_boxes) {
        int n = boxaGetCount(word_boxes);
        for (int i = 0; i < n; ++i) {
            BOX* box = boxaGetBox(word_boxes, i, L_CLONE);
            if (!box) continue;

            tess_api->SetRectangle(box->x, box->y, box->w, box->h);
            char* text = tess_api->GetUTF8Text();
            if (text) {
                std::string word(text);
                // Trim whitespace.
                while (!word.empty() && (word.back() == '\n' || word.back() == ' ')) {
                    word.pop_back();
                }
                if (!word.empty()) {
                    TextBlock block;
                    block.text = word;
                    block.x = box->x;
                    block.y = box->y;
                    block.w = box->w;
                    block.h = box->h;
                    blocks.push_back(std::move(block));
                }
                delete[] text;
            }
            boxDestroy(&box);
        }
        boxaDestroy(&word_boxes);
    }

    tess_api->Clear();
    return blocks;
}
