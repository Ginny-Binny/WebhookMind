#pragma once

#include <string>
#include <memory>

class TemplateCache {
public:
    explicit TemplateCache(const std::string& redis_addr, int redis_port = 6379);
    ~TemplateCache();

    // Returns the cached FieldPositionMap JSON, or empty string on miss.
    std::string get(const std::string& template_id);

    // Stores a FieldPositionMap JSON for a template_id.
    bool set(const std::string& template_id, const std::string& field_map_json);

    bool is_connected() const;

private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};
