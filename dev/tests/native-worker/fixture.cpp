// Deliberately synthetic C ABI fixture. No model, tensor, GPU, network, payment
// or privacy qualification. It verifies the actual worker's native call order.
#include "llama.h"
#include <chrono>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <filesystem>
#include <string>
#include <thread>
#include <vector>
#define EXPORT extern "C"

struct llama_model {};
struct llama_vocab {};
struct llama_memory_i { bool cleared = false; };
struct llama_context { llama_context_params params;std::string prompt;size_t next = 0;llama_memory_i memory; };
using children = std::vector<llama_sampler *>;
static llama_sampler * new_sampler() {return new llama_sampler{nullptr, new children{}};}
static std::string audit_path;
static std::filesystem::path utf8path(const std::string & s) {return std::filesystem::path(std::u8string(reinterpret_cast<const char8_t *>(s.data()), s.size()));}
static llama_model model;
static llama_vocab vocab;
static bool context_init_null = false;
static void audit(const char * stage) {std::ofstream f(utf8path(audit_path), std::ios::app);f << stage << '\n';f.flush();if (!f.good()) std::_Exit(91);}

EXPORT void llama_log_set(ggml_log_callback, void *) {}
EXPORT void ggml_log_set(ggml_log_callback, void *) {}
EXPORT void ggml_backend_load_all_from_path(const char *) {}
EXPORT ggml_backend_dev_t ggml_backend_dev_by_name(const char *) {return nullptr;}
EXPORT enum ggml_backend_dev_type ggml_backend_dev_type(ggml_backend_dev_t) {return GGML_BACKEND_DEVICE_TYPE_CPU;}
EXPORT void llama_backend_init() {}
EXPORT llama_model_params llama_model_default_params() {return {};}
EXPORT llama_model * llama_model_load_from_file(const char * path, llama_model_params params) {
    if (params.n_gpu_layers != 0 || !params.devices || params.devices[0] || params.load_mode != LLAMA_LOAD_MODE_MMAP || params.split_mode != LLAMA_SPLIT_MODE_NONE) return nullptr;
    audit_path = std::string(path) + ".audit";audit("MODEL_LOAD_SYNTHETIC");return &model;
}
EXPORT const llama_vocab * llama_model_get_vocab(const llama_model *) {return &vocab;}
EXPORT bool llama_model_has_encoder(const llama_model *) {return false;}
EXPORT llama_context_params llama_context_default_params() {return {};}
EXPORT llama_context * llama_init_from_model(llama_model *, llama_context_params params) {
    if (context_init_null) {audit("CONTEXT_INIT_NULL_SYNTHETIC");return nullptr;}
    if (!params.abort_callback || params.n_seq_max != 1 || params.offload_kqv || params.op_offload) return nullptr;
    audit("CONTEXT_SYNTHETIC");auto * ctx = new llama_context{};ctx->params = params;return ctx;
}
EXPORT uint32_t llama_n_ctx(const llama_context * ctx) {return ctx->params.n_ctx;}
EXPORT int32_t llama_chat_apply_template(const char * name, const llama_chat_message * messages, size_t count, bool, char * out, int32_t capacity) {
    if (std::string(name) != "chatml") return -1;
    std::string text;
    for (size_t i = 0; i < count; ++i) {text += messages[i].role;text += ':';text += messages[i].content;text += '\n';}
    if (out && capacity >= static_cast<int32_t>(text.size())) std::memcpy(out, text.data(), text.size());
    return static_cast<int32_t>(text.size());
}
EXPORT int32_t llama_tokenize(const llama_vocab *, const char * text, int32_t size, llama_token * out, int32_t capacity, bool, bool) {
    context_init_null = std::string(text, size).find("CONTEXT_NULL_FIXTURE") != std::string::npos;
    audit("TOKENIZE_SYNTHETIC");if (capacity < size) return -size;
    for (int32_t i = 0; i < size; ++i) out[i] = static_cast<unsigned char>(text[i]) + 1;return size;
}
EXPORT llama_batch llama_batch_get_one(llama_token * tokens, int32_t count) {llama_batch b{};b.token = tokens;b.n_tokens = count;return b;}
EXPORT int32_t llama_decode(llama_context * ctx, llama_batch batch) {
    audit("DECODE_SYNTHETIC");
    for (int32_t i = 0; i < batch.n_tokens; ++i) ctx->prompt += static_cast<char>(batch.token[i] - 1);
    if (ctx->prompt.find("CRASH_NATIVE_FIXTURE") != std::string::npos) std::_Exit(77);
    if (ctx->prompt.find("WAIT_NATIVE_FIXTURE") != std::string::npos) {
        for (int i = 0; i < 200; ++i) {if (ctx->params.abort_callback(ctx->params.abort_callback_data)) return 2;std::this_thread::sleep_for(std::chrono::milliseconds(5));}
    }
    return ctx->params.abort_callback(ctx->params.abort_callback_data) ? 2 : 0;
}
EXPORT llama_sampler_chain_params llama_sampler_chain_default_params() {return {};}
EXPORT llama_sampler * llama_sampler_chain_init(llama_sampler_chain_params) {return new_sampler();}
EXPORT void llama_sampler_chain_add(llama_sampler * parent, llama_sampler * child) {static_cast<children *>(parent->ctx)->push_back(child);}
EXPORT llama_sampler * llama_sampler_init_greedy() {return new_sampler();}
EXPORT llama_sampler * llama_sampler_init_top_p(float, size_t) {return new_sampler();}
EXPORT llama_sampler * llama_sampler_init_temp(float) {return new_sampler();}
EXPORT llama_sampler * llama_sampler_init_dist(uint32_t) {return new_sampler();}
EXPORT llama_token llama_sampler_sample(llama_sampler *, llama_context * ctx, int32_t) {
    static const std::string output = "hello<END>hidden";
    return ctx->next < output.size() ? static_cast<unsigned char>(output[ctx->next++]) + 1 : 0;
}
EXPORT bool llama_vocab_is_eog(const llama_vocab *, llama_token token) {return token == 0;}
EXPORT int32_t llama_token_to_piece(const llama_vocab *, llama_token token, char * out, int32_t capacity, int32_t, bool) {
    if (capacity < 1) return -1;out[0] = static_cast<char>(token - 1);return 1;
}
EXPORT void llama_sampler_free(llama_sampler * sampler) {auto * list = static_cast<children *>(sampler->ctx);for (auto * child : *list) llama_sampler_free(child);delete list;delete sampler;}
EXPORT void llama_synchronize(llama_context *) {
    audit("SYNCHRONIZE_SYNTHETIC");
    for (int i = 0; i < 2500; ++i) {
        if (std::ifstream(utf8path(audit_path + ".allow-clear")).good()) return;
        std::this_thread::sleep_for(std::chrono::milliseconds(2));
    }
    std::_Exit(90);
}
EXPORT llama_memory_t llama_get_memory(const llama_context * ctx) {return const_cast<llama_memory_i *>(&ctx->memory);}
EXPORT void llama_memory_clear(llama_memory_t memory, bool data) {if (!data) std::_Exit(88);memory->cleared = true;audit("CLEAR_SYNTHETIC");}
EXPORT void llama_free(llama_context * ctx) {if (!ctx->memory.cleared) std::_Exit(89);audit("FREE_SYNTHETIC");delete ctx;}
