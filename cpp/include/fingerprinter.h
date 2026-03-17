#pragma once

#include "extractor.h"
#include <string>
#include <vector>

// Computes a structural fingerprint (SHA-256 hex) from text blocks.
std::string compute_fingerprint(const std::vector<TextBlock>& blocks, double page_width, double page_height);
