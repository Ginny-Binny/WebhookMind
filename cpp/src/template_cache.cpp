#include "template_cache.h"

#include <hiredis/hiredis.h>
#include <iostream>

struct TemplateCache::Impl {
    redisContext* ctx = nullptr;
    bool connected = false;
};

TemplateCache::TemplateCache(const std::string& redis_addr, int redis_port)
    : impl_(std::make_unique<Impl>()) {

    struct timeval timeout = {2, 0}; // 2 second timeout
    impl_->ctx = redisConnectWithTimeout(redis_addr.c_str(), redis_port, timeout);

    if (!impl_->ctx || impl_->ctx->err) {
        if (impl_->ctx) {
            std::cerr << "[WARN] redis connect failed: " << impl_->ctx->errstr << std::endl;
            redisFree(impl_->ctx);
            impl_->ctx = nullptr;
        } else {
            std::cerr << "[WARN] redis connect failed: allocation error" << std::endl;
        }
        impl_->connected = false;
    } else {
        impl_->connected = true;
    }
}

TemplateCache::~TemplateCache() {
    if (impl_->ctx) {
        redisFree(impl_->ctx);
    }
}

bool TemplateCache::is_connected() const {
    return impl_->connected;
}

std::string TemplateCache::get(const std::string& template_id) {
    if (!impl_->connected) return "";

    std::string key = "webhookmind:template:" + template_id;
    auto* reply = static_cast<redisReply*>(
        redisCommand(impl_->ctx, "GET %s", key.c_str())
    );

    if (!reply) {
        impl_->connected = false;
        return "";
    }

    std::string result;
    if (reply->type == REDIS_REPLY_STRING && reply->str) {
        result = std::string(reply->str, reply->len);
    }

    freeReplyObject(reply);
    return result;
}

bool TemplateCache::set(const std::string& template_id, const std::string& field_map_json) {
    if (!impl_->connected) return false;

    std::string key = "webhookmind:template:" + template_id;
    auto* reply = static_cast<redisReply*>(
        redisCommand(impl_->ctx, "SET %s %s", key.c_str(), field_map_json.c_str())
    );

    if (!reply) {
        impl_->connected = false;
        return false;
    }

    bool ok = (reply->type == REDIS_REPLY_STATUS);
    freeReplyObject(reply);
    return ok;
}
