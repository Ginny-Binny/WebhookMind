#pragma once

#include "extractor.h"
#include <string>
#include <vector>

std::vector<TextBlock> extract_pdf(const std::string& file_path);
