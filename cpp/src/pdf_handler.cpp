#include "pdf_handler.h"

#include <poppler/cpp/poppler-document.h>
#include <poppler/cpp/poppler-page.h>
#include <memory>
#include <iostream>

std::vector<TextBlock> extract_pdf(const std::string& file_path) {
    std::vector<TextBlock> blocks;

    auto doc = std::unique_ptr<poppler::document>(
        poppler::document::load_from_file(file_path)
    );

    if (!doc) {
        std::cerr << "[ERROR] failed to open PDF: " << file_path << std::endl;
        return blocks;
    }

    int num_pages = doc->pages();
    for (int i = 0; i < num_pages; ++i) {
        auto page = std::unique_ptr<poppler::page>(doc->create_page(i));
        if (!page) continue;

        auto text_list = page->text_list();
        for (const auto& item : text_list) {
            TextBlock block;
            block.text = item.text().to_latin1();
            auto bbox = item.bbox();
            block.x = bbox.x();
            block.y = bbox.y();
            block.w = bbox.width();
            block.h = bbox.height();
            blocks.push_back(std::move(block));
        }
    }

    return blocks;
}
