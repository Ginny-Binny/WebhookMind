#include "fingerprinter.h"

#include <openssl/evp.h>
#include <algorithm>
#include <cmath>
#include <sstream>
#include <iomanip>

// Classify a text block based on content and position.
static std::string classify_block(const TextBlock& block, double norm_y, double page_height) {
    // Header: top 15% of page.
    if (norm_y < 0.15) {
        return "header";
    }

    // Footer: bottom 10% of page.
    if (norm_y > 0.90) {
        return "footer";
    }

    // Label: ends with ':', all caps, or short length < 30 chars.
    const auto& text = block.text;
    if (!text.empty() && text.back() == ':') {
        return "label";
    }

    bool all_upper = true;
    for (char c : text) {
        if (std::isalpha(c) && !std::isupper(c)) {
            all_upper = false;
            break;
        }
    }
    if (all_upper && text.length() < 30 && text.length() > 1) {
        return "label";
    }

    return "value";
}

static double round2(double v) {
    return std::round(v * 100.0) / 100.0;
}

std::string compute_fingerprint(const std::vector<TextBlock>& blocks, double page_width, double page_height) {
    if (blocks.empty() || page_width <= 0 || page_height <= 0) {
        return "";
    }

    // Step 1: Normalize and sort.
    struct NormBlock {
        std::string text;
        double norm_x, norm_y, norm_w;
    };

    std::vector<NormBlock> norm_blocks;
    norm_blocks.reserve(blocks.size());

    for (const auto& b : blocks) {
        NormBlock nb;
        nb.text = b.text;
        nb.norm_x = round2(b.x / page_width);
        nb.norm_y = round2(b.y / page_height);
        nb.norm_w = round2(b.w / page_width);
        norm_blocks.push_back(std::move(nb));
    }

    // Sort by (y, x) — top-to-bottom, left-to-right.
    std::sort(norm_blocks.begin(), norm_blocks.end(), [](const NormBlock& a, const NormBlock& b) {
        if (a.norm_y != b.norm_y) return a.norm_y < b.norm_y;
        return a.norm_x < b.norm_x;
    });

    // Step 2 & 3: Classify and build structural signature.
    std::ostringstream sig;
    for (size_t i = 0; i < norm_blocks.size(); ++i) {
        const auto& nb = norm_blocks[i];

        // Create a temporary TextBlock for classification.
        TextBlock tmp;
        tmp.text = nb.text;
        std::string classification = classify_block(tmp, nb.norm_y, page_height);

        if (i > 0) sig << "|";
        sig << classification << ":"
            << std::fixed << std::setprecision(2)
            << nb.norm_x << ":"
            << nb.norm_y << ":"
            << nb.norm_w;
    }

    std::string signature = sig.str();

    // Step 4: SHA-256 hash.
    unsigned char hash[EVP_MAX_MD_SIZE];
    unsigned int hash_len = 0;

    EVP_MD_CTX* ctx = EVP_MD_CTX_new();
    if (ctx) {
        EVP_DigestInit_ex(ctx, EVP_sha256(), nullptr);
        EVP_DigestUpdate(ctx, signature.data(), signature.size());
        EVP_DigestFinal_ex(ctx, hash, &hash_len);
        EVP_MD_CTX_free(ctx);
    }

    // Convert to hex string (64 chars for SHA-256).
    std::ostringstream hex;
    for (unsigned int i = 0; i < hash_len; ++i) {
        hex << std::hex << std::setw(2) << std::setfill('0') << static_cast<int>(hash[i]);
    }

    return hex.str();
}
