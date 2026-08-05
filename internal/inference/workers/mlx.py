import base64
import json
import math
import os
import random
import sys
import time
import traceback

import mlx.core as mx
import mlx.nn as nn


PROTOCOL_SCHEMA = 1


def emit(kind, **payload):
    frame = {"kind": kind, "schema": PROTOCOL_SCHEMA}
    frame.update(payload)
    print(json.dumps(frame, separators=(",", ":")), flush=True)


class Attention(nn.Module):
    def __init__(self, hidden, heads, kv_heads):
        super().__init__()
        self.heads = heads
        self.kv_heads = kv_heads
        self.head_dim = hidden // heads
        kv_width = self.head_dim * kv_heads
        self.q_proj = nn.Linear(hidden, hidden, bias=False)
        self.k_proj = nn.Linear(hidden, kv_width, bias=False)
        self.v_proj = nn.Linear(hidden, kv_width, bias=False)
        self.o_proj = nn.Linear(hidden, hidden, bias=False)
        self.rope = nn.RoPE(self.head_dim, traditional=False, base=10000)

    def __call__(self, value, cache=None):
        batch, length, _ = value.shape
        offset = 0 if cache is None else cache[0].shape[2]
        query = self.q_proj(value).reshape(batch, length, self.heads, self.head_dim).transpose(0, 2, 1, 3)
        key = self.k_proj(value).reshape(batch, length, self.kv_heads, self.head_dim).transpose(0, 2, 1, 3)
        val = self.v_proj(value).reshape(batch, length, self.kv_heads, self.head_dim).transpose(0, 2, 1, 3)
        query = self.rope(query, offset=offset)
        key = self.rope(key, offset=offset)
        if cache is not None:
            key = mx.concatenate([cache[0], key], axis=2)
            val = mx.concatenate([cache[1], val], axis=2)
        if length > 1:
            attended = mx.fast.scaled_dot_product_attention(
                query, key, val, scale=self.head_dim ** -0.5, mask="causal"
            )
        else:
            attended = mx.fast.scaled_dot_product_attention(
                query, key, val, scale=self.head_dim ** -0.5
            )
        attended = attended.transpose(0, 2, 1, 3).reshape(batch, length, -1)
        return self.o_proj(attended), (key, val)


class FeedForward(nn.Module):
    def __init__(self, hidden, intermediate):
        super().__init__()
        self.gate = nn.Linear(hidden, intermediate, bias=False)
        self.up = nn.Linear(hidden, intermediate, bias=False)
        self.down = nn.Linear(intermediate, hidden, bias=False)

    def __call__(self, value):
        return self.down(nn.silu(self.gate(value)) * self.up(value))


class DecoderBlock(nn.Module):
    def __init__(self, hidden, intermediate, heads, kv_heads):
        super().__init__()
        self.attention_norm = nn.RMSNorm(hidden, eps=1e-5)
        self.attention = Attention(hidden, heads, kv_heads)
        self.ffn_norm = nn.RMSNorm(hidden, eps=1e-5)
        self.feed_forward = FeedForward(hidden, intermediate)

    def __call__(self, value, cache=None):
        attended, next_cache = self.attention(self.attention_norm(value), cache)
        value = value + attended
        return value + self.feed_forward(self.ffn_norm(value)), next_cache


class DecoderLM(nn.Module):
    def __init__(self, architecture):
        super().__init__()
        vocabulary = architecture["vocabulary_size"]
        hidden = architecture["hidden_size"]
        self.tie_embeddings = architecture["tie_embeddings"]
        self.embedding = nn.Embedding(vocabulary, hidden)
        self.layers = [
            DecoderBlock(
                hidden,
                architecture["intermediate_size"],
                architecture["attention_heads"],
                architecture["key_value_heads"],
            )
            for _ in range(architecture["layers"])
        ]
        self.norm = nn.RMSNorm(hidden, eps=1e-5)
        if not self.tie_embeddings:
            self.output = nn.Linear(hidden, vocabulary, bias=False)

    def __call__(self, tokens, cache=None):
        value = self.embedding(tokens)
        next_cache = []
        for index, layer in enumerate(self.layers):
            layer_cache = None if cache is None else cache[index]
            value, layer_cache = layer(value, layer_cache)
            next_cache.append(layer_cache)
        value = self.norm(value)
        if self.tie_embeddings:
            return self.embedding.as_linear(value), next_cache
        return self.output(value), next_cache


class ByteTokenizer:
    pad_id = 0
    bos_id = 1
    eos_id = 2
    byte_offset = 3

    def encode(self, text):
        return [byte + self.byte_offset for byte in text.encode("utf-8")]

    def decode_token(self, token):
        if self.byte_offset <= token < self.byte_offset + 256:
            return bytes([token - self.byte_offset])
        return b""


def sample(logits, temperature, top_p, generator):
    values = [float(value) for value in logits.tolist()]
    if temperature == 0:
        return max(range(len(values)), key=values.__getitem__)
    scaled = [value / temperature for value in values]
    maximum = max(scaled)
    probabilities = [math.exp(value - maximum) for value in scaled]
    total = sum(probabilities)
    probabilities = [value / total for value in probabilities]
    order = sorted(range(len(probabilities)), key=probabilities.__getitem__, reverse=True)
    kept = []
    cumulative = 0.0
    for index in order:
        if kept and cumulative >= top_p:
            break
        probability = probabilities[index]
        kept.append((index, probability))
        cumulative += probability
    draw = generator.random() * cumulative
    cumulative = 0.0
    for index, probability in kept:
        cumulative += probability
        if draw <= cumulative:
            return index
    return kept[-1][0]


class Generator:
    def __init__(self, weights_path, config_path, tokenizer_path):
        with open(config_path, "r", encoding="utf-8") as stream:
            config = json.load(stream)
        with open(tokenizer_path, "r", encoding="utf-8") as stream:
            tokenizer = json.load(stream)
        if config.get("kind") != "waldo-mlx-model-config" or config.get("schema") != 1:
            raise ValueError("unsupported WALDO MLX model configuration")
        if tokenizer.get("kind") != "waldo-byte-tokenizer" or tokenizer.get("schema") != 1:
            raise ValueError("unsupported WALDO tokenizer artifact")
        architecture = config["architecture"]
        if (
            tokenizer.get("name") != "byte"
            or tokenizer.get("revision") != "builtin-byte-schema-1"
            or tokenizer.get("vocabulary_size") != 259
            or architecture.get("vocabulary_size") != 259
        ):
            raise ValueError("MLX chat requires byte@builtin-byte-schema-1 with vocabulary size 259")
        self.context_tokens = int(architecture["context_tokens"])
        self.tokenizer = ByteTokenizer()
        self.model = DecoderLM(architecture)
        self.model.load_weights(weights_path)
        mx.eval(self.model.parameters())

    def generate(self, request):
        prompt = request.get("prompt", "")
        maximum = int(request["max_tokens"])
        temperature = float(request["temperature"])
        top_p = float(request["top_p"])
        seed = request.get("seed")
        generator = random.Random(seed if seed is not None else int.from_bytes(os.urandom(16), "big"))
        tokens = self.tokenizer.encode(prompt, add_eos=False)
        if not tokens:
            tokens = [self.tokenizer.bos_id]
        tokens = tokens[-self.context_tokens:]
        started = time.perf_counter()
        produced = 0
        reason = "max_tokens"
        inputs = mx.array([tokens], dtype=mx.int32)
        logits, cache = self.model.generate(inputs)
        mx.eval(logits, cache)
        for _ in range(maximum):
            token = sample(logits[0, -1], temperature, top_p, generator)
            tokens.append(token)
            if token == self.tokenizer.eos_id:
                reason = "eos"
                break
            data = self.tokenizer.decode_token(token)
            if data:
                produced += 1
                emit("token", data=base64.b64encode(data).decode("ascii"))
            if len(tokens) >= self.context_tokens:
                tokens = tokens[-self.context_tokens:]
                inputs = mx.array([tokens], dtype=mx.int32)
                logits, cache = self.model.generate(inputs)
            else:
                inputs = mx.array([[token]], dtype=mx.int32)
                logits, cache = self.model.generate(inputs, cache)
            mx.eval(logits, cache)
        emit(
            "complete",
            tokens=produced,
            finish_reason=reason,
            duration_ms=int((time.perf_counter() - started) * 1000),
        )


def run():
    if len(sys.argv) != 4:
        raise ValueError("MLX chat worker requires weights, config, and tokenizer paths")
    generator = Generator(sys.argv[1], sys.argv[2], sys.argv[3])
    emit("ready", context_tokens=generator.context_tokens)
    for line in sys.stdin:
        request = json.loads(line)
        if request.get("schema") != PROTOCOL_SCHEMA or request.get("kind") != "generate":
            raise ValueError("unsupported MLX chat request")
        generator.generate(request)


Attention = WALDO_SHARED_ATTENTION
FeedForward = WALDO_SHARED_FEED_FORWARD
DecoderBlock = WALDO_SHARED_DECODER_BLOCK
DecoderLM = WALDO_SHARED_DECODER_LM
ByteTokenizer = WALDO_SHARED_BYTE_TOKENIZER

try:
    run()
except Exception as error:
    traceback.print_exc(file=sys.stderr)
    emit("error", error=str(error))
    sys.exit(1)
