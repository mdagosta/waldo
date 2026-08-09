# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

import hashlib
import json
import math
import os
import shutil
import struct
import sys
import time
import traceback

import torch
import torch.nn as nn
import torch.nn.functional as functional


PROTOCOL_SCHEMA = 1
WORKER_REVISION = "builtin-pytorch-worker-schema-1-r2"
TORCHTITAN_REVISION = "builtin-torchtitan-worker-schema-1-r2"
IS_PRIMARY = True


def emit(kind, **payload):
    if not IS_PRIMARY:
        return
    frame = {"kind": kind, "schema": PROTOCOL_SCHEMA}
    frame.update(payload)
    print(json.dumps(frame, separators=(",", ":")), flush=True)


def artifact(path, logical_path):
    digest = hashlib.sha256()
    size = 0
    with open(path, "rb") as stream:
        while True:
            block = stream.read(1024 * 1024)
            if not block:
                break
            digest.update(block)
            size += len(block)
    return {"path": logical_path, "sha256": digest.hexdigest(), "bytes": size}


def write_json(path, value):
    temporary = path + ".tmp"
    with open(temporary, "w", encoding="utf-8") as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)


def commit_directory(temporary, destination):
    for root, _, files in os.walk(temporary):
        for name in files:
            descriptor = os.open(os.path.join(root, name), os.O_RDONLY)
            try:
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
    descriptor = os.open(temporary, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    os.replace(temporary, destination)
    descriptor = os.open(os.path.dirname(destination), os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


SAFE_DTYPES = {
    torch.float32: "F32",
    torch.float16: "F16",
    torch.bfloat16: "BF16",
}
SAFE_TORCH_DTYPES = {value: key for key, value in SAFE_DTYPES.items()}


def save_safetensors(path, tensors, metadata):
    names = sorted(tensors)
    header = {"__metadata__": metadata}
    payloads = []
    offset = 0
    for name in names:
        tensor = tensors[name].detach().to("cpu").contiguous()
        dtype = SAFE_DTYPES.get(tensor.dtype)
        if dtype is None:
            raise ValueError(f"cannot write Safetensors dtype {tensor.dtype} for {name}")
        payload = tensor.view(torch.uint8).numpy().tobytes()
        header[name] = {
            "dtype": dtype,
            "shape": list(tensor.shape),
            "data_offsets": [offset, offset + len(payload)],
        }
        payloads.append(payload)
        offset += len(payload)
    encoded = json.dumps(header, separators=(",", ":"), sort_keys=True).encode("utf-8")
    encoded += b" " * ((8 - len(encoded) % 8) % 8)
    temporary = path + ".tmp"
    with open(temporary, "wb") as stream:
        stream.write(struct.pack("<Q", len(encoded)))
        stream.write(encoded)
        for payload in payloads:
            stream.write(payload)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)


def load_safetensors(path):
    with open(path, "rb") as stream:
        header_length_data = stream.read(8)
        if len(header_length_data) != 8:
            raise ValueError("initialization Safetensors header is truncated")
        header_length = struct.unpack("<Q", header_length_data)[0]
        if header_length == 0 or header_length > 1024 * 1024 * 1024:
            raise ValueError(f"invalid initialization Safetensors header length {header_length}")
        header = json.loads(stream.read(header_length))
        payload = bytearray(stream.read())
    tensors = {}
    for name, descriptor in header.items():
        if name == "__metadata__":
            continue
        dtype = SAFE_TORCH_DTYPES.get(descriptor["dtype"])
        if dtype is None:
            raise ValueError(f"unsupported initialization Safetensors dtype {descriptor['dtype']}")
        start, end = descriptor["data_offsets"]
        if start < 0 or end < start or end > len(payload):
            raise ValueError(f"invalid initialization offsets for {name}")
        value = torch.frombuffer(payload[start:end], dtype=dtype).clone()
        tensors[name] = value.reshape(descriptor["shape"])
    return tensors


class RMSNorm(nn.Module):
    def __init__(self, hidden, epsilon=1e-5):
        super().__init__()
        self.weight = nn.Parameter(torch.ones(hidden))
        self.epsilon = epsilon

    def forward(self, value):
        normalized = value.float() * torch.rsqrt(value.float().pow(2).mean(-1, keepdim=True) + self.epsilon)
        return normalized.to(value.dtype) * self.weight


def rotate_half(value):
    first = value[..., : value.shape[-1] // 2]
    second = value[..., value.shape[-1] // 2 :]
    return torch.cat((-second, first), dim=-1)


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

    def rope(self, value):
        length = value.shape[2]
        positions = torch.arange(length, device=value.device, dtype=torch.float32)
        frequencies = 1.0 / (10000.0 ** (torch.arange(0, self.head_dim, 2, device=value.device, dtype=torch.float32) / self.head_dim))
        angles = torch.outer(positions, frequencies)
        angles = torch.cat((angles, angles), dim=-1).to(value.dtype)[None, None, :, :]
        return value * angles.cos() + rotate_half(value) * angles.sin()

    def forward(self, value):
        batch, length, _ = value.shape
        query = self.q_proj(value).reshape(batch, length, self.heads, self.head_dim).transpose(1, 2)
        key = self.k_proj(value).reshape(batch, length, self.kv_heads, self.head_dim).transpose(1, 2)
        val = self.v_proj(value).reshape(batch, length, self.kv_heads, self.head_dim).transpose(1, 2)
        query = self.rope(query)
        key = self.rope(key)
        if self.heads != self.kv_heads:
            repeats = self.heads // self.kv_heads
            key = key.repeat_interleave(repeats, dim=1)
            val = val.repeat_interleave(repeats, dim=1)
        attended = functional.scaled_dot_product_attention(query, key, val, is_causal=True)
        attended = attended.transpose(1, 2).reshape(batch, length, -1)
        return self.o_proj(attended)


class FeedForward(nn.Module):
    def __init__(self, hidden, intermediate):
        super().__init__()
        self.gate = nn.Linear(hidden, intermediate, bias=False)
        self.up = nn.Linear(hidden, intermediate, bias=False)
        self.down = nn.Linear(intermediate, hidden, bias=False)

    def forward(self, value):
        return self.down(functional.silu(self.gate(value)) * self.up(value))


class DecoderBlock(nn.Module):
    def __init__(self, hidden, intermediate, heads, kv_heads):
        super().__init__()
        self.attention_norm = RMSNorm(hidden)
        self.attention = Attention(hidden, heads, kv_heads)
        self.ffn_norm = RMSNorm(hidden)
        self.feed_forward = FeedForward(hidden, intermediate)

    def forward(self, value):
        value = value + self.attention(self.attention_norm(value))
        return value + self.feed_forward(self.ffn_norm(value))


class DecoderLM(nn.Module):
    def __init__(self, architecture):
        super().__init__()
        vocabulary = architecture["vocabulary_size"]
        hidden = architecture["hidden_size"]
        self.tie_embeddings = architecture["tie_embeddings"]
        self.embedding = nn.Embedding(vocabulary, hidden)
        self.layers = nn.ModuleList([
            DecoderBlock(
                hidden,
                architecture["intermediate_size"],
                architecture["attention_heads"],
                architecture["key_value_heads"],
            )
            for _ in range(architecture["layers"])
        ])
        self.norm = RMSNorm(hidden)
        if not self.tie_embeddings:
            self.output = nn.Linear(hidden, vocabulary, bias=False)

    def forward(self, tokens):
        value = self.embedding(tokens)
        for layer in self.layers:
            value = layer(value)
        value = self.norm(value)
        if self.tie_embeddings:
            return functional.linear(value, self.embedding.weight)
        return self.output(value)


class ByteTokenizer:
    pad_id = 0
    bos_id = 1
    eos_id = 2

    def encode(self, text):
        return [byte + 3 for byte in text.encode("utf-8")] + [self.eos_id]


class Trainer:
    def __init__(self, begin, artifact_directory, artifact_prefix, device_name):
        self.begin = begin
        self.architecture = begin["architecture"]
        self.parameters = begin["parameters"]
        self.artifact_directory = artifact_directory
        self.artifact_prefix = artifact_prefix.replace(os.sep, "/").strip("/")
        self.distributed = device_name == "torchtitan"
        self.rank = torch.distributed.get_rank() if self.distributed else 0
        self.world_size = torch.distributed.get_world_size() if self.distributed else 1
        if self.distributed:
            local_rank = int(os.environ["LOCAL_RANK"])
            self.device = torch.device(f"cuda:{local_rank}")
            torch.cuda.set_device(self.device)
            from torchtitan.distributed import ParallelDims

            self.parallel_dims = ParallelDims(
                dp_replicate=1,
                dp_shard=self.world_size,
                cp=1,
                tp=1,
                pp=1,
                ep=1,
                world_size=self.world_size,
            )
            self.parallel_dims.build_mesh()
        else:
            self.device = torch.device(device_name)
        self.sequence_length = self.parameters["sequence_length"]
        self.batch_size = self.parameters["batch_size"]
        self.target_steps = self.parameters["steps"]
        self.step_number = 0
        self.replay_steps = 0
        self.consumed_tokens = 0
        self.token_buffer = []
        self.batch = []
        self.checkpoints = []
        self.evaluations = []
        self.evaluation_sequences = []
        self.evaluation_record_count = 0
        self.evaluation_token_targets = 0
        self.final_loss = None
        self.started = time.perf_counter()

        tokenizer = self.architecture["tokenizer"]
        if (
            tokenizer["name"] != "byte"
            or tokenizer["revision"] != "builtin-byte-schema-1"
            or self.architecture["vocabulary_size"] != 259
        ):
            raise ValueError("PyTorch worker requires byte@builtin-byte-schema-1 with vocabulary_size 259")
        if self.device.type == "cuda" and not torch.cuda.is_available():
            raise ValueError("PyTorch worker selected CUDA but torch.cuda.is_available() is false")
        self.tokenizer = ByteTokenizer()
        torch.manual_seed(self.parameters["seed"])
        if self.device.type == "cuda":
            torch.cuda.manual_seed_all(self.parameters["seed"])
        self.model = DecoderLM(self.architecture)
        self.initialization = begin.get("initialization")
        if self.initialization is not None:
            missing, unexpected = self.model.load_state_dict(load_safetensors(self.initialization["path"]), strict=False)
            if missing or unexpected:
                raise ValueError(f"initialization weights do not match architecture: missing={missing}, unexpected={unexpected}")
        self.resume = begin.get("resume")
        self.resume_paths = None
        if self.resume is not None:
            self.resume_paths = {os.path.basename(path): path for path in self.resume["paths"]}
            required = {"model.safetensors", "runtime.pt", "state.json"}
            if set(self.resume_paths) != required:
                raise ValueError(f"PyTorch checkpoint requires {sorted(required)}, found {sorted(self.resume_paths)}")
            missing, unexpected = self.model.load_state_dict(load_safetensors(self.resume_paths["model.safetensors"]), strict=False)
            if missing or unexpected:
                raise ValueError(f"resume weights do not match architecture: missing={missing}, unexpected={unexpected}")
        dtype_name = self.architecture["parameter_dtype"]
        dtype = {"float32": torch.float32, "float16": torch.float16, "bfloat16": torch.bfloat16}[dtype_name]
        if self.device.type == "cpu" and dtype == torch.float16:
            raise ValueError("float16 training is not supported by the PyTorch CPU adapter; use bfloat16 or float32")
        self.model.to(device=self.device, dtype=dtype)
        if self.distributed:
            from torch.distributed._composable.fsdp import fully_shard

            fsdp_mesh = self.parallel_dims.get_mesh("fsdp")
            for layer in self.model.layers:
                fully_shard(layer, mesh=fsdp_mesh)
            fully_shard(self.model, mesh=fsdp_mesh)
        optimizer_parameters = self.parameters["optimizer"]
        self.optimizer = torch.optim.AdamW(
            self.model.parameters(),
            lr=self.parameters["learning_rate"],
            betas=(optimizer_parameters["beta1"], optimizer_parameters["beta2"]),
            eps=optimizer_parameters["epsilon"],
            weight_decay=optimizer_parameters["weight_decay"],
        )
        if self.resume is not None:
            self.restore_checkpoint()

    def logical(self, name):
        return "/".join(part for part in (self.artifact_prefix, name) if part)

    def synchronize(self):
        if self.device.type == "cuda":
            torch.cuda.synchronize(self.device)

    def learning_rate(self, step):
        schedule = self.parameters["schedule"]
        base = self.parameters["learning_rate"]
        warmup = schedule["warmup_steps"]
        if warmup > 0 and step <= warmup:
            return base * step / warmup
        decay_steps = max(1, self.target_steps - warmup)
        progress = min(1.0, max(0.0, (step - warmup) / decay_steps))
        ratio = schedule["minimum_rate_ratio"] + (1.0 - schedule["minimum_rate_ratio"]) * 0.5 * (1.0 + math.cos(math.pi * progress))
        return base * ratio

    def add_record(self, record):
        if self.step_number >= self.target_steps:
            return
        self.token_buffer.extend(self.tokenizer.encode(record["text"]))
        window = self.sequence_length + 1
        while len(self.token_buffer) >= window and self.step_number < self.target_steps:
            self.add_sequence(self.token_buffer[:window], self.sequence_length)
            del self.token_buffer[: self.sequence_length]

    def add_evaluation_record(self, record):
        self.evaluation_record_count += 1
        tokens = self.tokenizer.encode(record["text"])
        window = self.sequence_length + 1
        while len(tokens) > 1:
            piece = tokens[:window]
            valid_targets = len(piece) - 1
            padded = piece + [self.tokenizer.pad_id] * (window - len(piece))
            mask = [1.0] * valid_targets + [0.0] * (self.sequence_length - valid_targets)
            self.evaluation_sequences.append((padded, mask))
            self.evaluation_token_targets += valid_targets
            del tokens[: self.sequence_length]

    def add_sequence(self, tokens, valid_targets):
        if valid_targets <= 0 or self.step_number >= self.target_steps:
            return
        window = self.sequence_length + 1
        padded = tokens + [self.tokenizer.pad_id] * (window - len(tokens))
        mask = [1.0] * valid_targets + [0.0] * (self.sequence_length - valid_targets)
        self.batch.append((padded, mask))
        if len(self.batch) >= self.batch_size:
            self.train_batch()

    def train_batch(self):
        if not self.batch or self.step_number >= self.target_steps:
            self.batch = []
            return
        if self.replay_steps > 0:
            self.replay_steps -= 1
            self.batch = []
            return
        tokens = torch.tensor([item[0] for item in self.batch], dtype=torch.long, device=self.device)
        mask = torch.tensor([item[1] for item in self.batch], dtype=torch.float32, device=self.device)
        inputs = tokens[:, :-1]
        targets = tokens[:, 1:]
        next_step = self.step_number + 1
        current_learning_rate = self.learning_rate(next_step)
        for group in self.optimizer.param_groups:
            group["lr"] = current_learning_rate
        self.optimizer.zero_grad(set_to_none=True)
        logits = self.model(inputs)
        losses = functional.cross_entropy(logits.float().reshape(-1, logits.shape[-1]), targets.reshape(-1), reduction="none")
        loss = (losses.reshape_as(mask) * mask).sum() / mask.sum()
        loss.backward()
        self.optimizer.step()
        self.synchronize()
        loss_value = float(loss.detach().cpu().item())
        valid_tokens = int(mask.sum().detach().cpu().item())
        self.step_number = next_step
        self.consumed_tokens += valid_tokens
        self.final_loss = loss_value
        self.batch = []
        elapsed = max(time.perf_counter() - self.started, 1e-9)
        throughput = self.consumed_tokens / elapsed
        eta = int(max(0.0, (self.target_steps - self.step_number) * elapsed / self.step_number))
        report_every = max(1, self.target_steps // 100)
        if self.step_number == 1 or self.step_number == self.target_steps or self.step_number % report_every == 0:
            emit(
                "event",
                event={
                    "kind": "progress",
                    "message": f"step {self.step_number}/{self.target_steps}, loss {loss_value:.4f}, {throughput:.0f} tokens/s",
                    "step": self.step_number,
                    "tokens": self.consumed_tokens,
                    "loss": loss_value,
                    "learning_rate": current_learning_rate,
                    "tokens_per_second": throughput,
                    "eta_seconds": eta,
                },
            )
        checkpoint_every = self.parameters["checkpoint_every"]
        if checkpoint_every > 0 and self.step_number % checkpoint_every == 0:
            self.save_checkpoint()
        evaluate_every = self.parameters["evaluate_every"]
        if evaluate_every > 0 and self.step_number % evaluate_every == 0:
            self.record_evaluation(loss_value)

    def save_weights(self, path, kind, step):
        if self.distributed:
            from torch.distributed.checkpoint.state_dict import (
                get_model_state_dict,
                StateDictOptions,
            )

            tensors = get_model_state_dict(
                self.model,
                options=StateDictOptions(full_state_dict=True, cpu_offload=True),
            )
        else:
            tensors = self.model.state_dict()
        if IS_PRIMARY:
            backend = "torchtitan" if self.distributed else "pytorch"
            revision = TORCHTITAN_REVISION if self.distributed else WORKER_REVISION
            save_safetensors(
                path,
                tensors,
                {
                    "format": "openwaldo",
                    "kind": kind,
                    "schema": "1",
                    "backend": backend,
                    "backend_revision": revision,
                    "architecture_sha256": self.begin["architecture_sha256"],
                    "run_id": self.begin["run_id"],
                    "step": str(step),
                },
            )
        if self.distributed:
            torch.distributed.barrier()

    def save_checkpoint(self):
        name = f"checkpoints/step-{self.step_number:08d}"
        path = os.path.join(self.artifact_directory, *name.split("/"))
        temporary = None
        if IS_PRIMARY:
            temporary = path + f".tmp-{os.getpid()}"
            if os.path.exists(temporary):
                shutil.rmtree(temporary)
            os.makedirs(temporary)
        if self.distributed:
            values = [temporary]
            torch.distributed.broadcast_object_list(values, src=0)
            temporary = values[0]
        weights_path = os.path.join(temporary, "model.safetensors")
        runtime_path = os.path.join(temporary, "runtime.pt")
        state_path = os.path.join(temporary, "state.json")
        backend_name = "torchtitan" if self.distributed else "pytorch"
        self.save_weights(weights_path, f"waldo-{backend_name}-checkpoint", self.step_number)
        random_state = {
            "cpu": torch.get_rng_state().cpu(),
            "cuda": torch.cuda.get_rng_state(self.device).cpu() if self.device.type == "cuda" else None,
        }
        if self.distributed:
            random_states = [None for _ in range(self.world_size)]
            torch.distributed.all_gather_object(random_states, random_state)
            from torch.distributed.checkpoint.state_dict import get_optimizer_state_dict, StateDictOptions

            optimizer_state = get_optimizer_state_dict(
                self.model,
                self.optimizer,
                options=StateDictOptions(full_state_dict=True, cpu_offload=True),
            )
        else:
            random_states = [random_state]
            optimizer_state = self.optimizer.state_dict()
        if IS_PRIMARY:
            torch.save({"optimizer": optimizer_state, "random_states": random_states}, runtime_path)
            write_json(
                state_path,
                {
                    "kind": "waldo-training-checkpoint",
                    "schema": 1,
                    "backend": backend_name,
                    "backend_revision": TORCHTITAN_REVISION if self.distributed else WORKER_REVISION,
                    "run_id": self.begin["run_id"],
                    "architecture_sha256": self.begin["architecture_sha256"],
                    "step": self.step_number,
                    "consumed_tokens": self.consumed_tokens,
                    "world_size": self.world_size,
                },
            )
            commit_directory(temporary, path)
        if self.distributed:
            torch.distributed.barrier()
        weights_path = os.path.join(path, "model.safetensors")
        runtime_path = os.path.join(path, "runtime.pt")
        state_path = os.path.join(path, "state.json")
        item = {
            "step": self.step_number,
            "tokens": self.consumed_tokens,
            "artifacts": [
                artifact(weights_path, self.logical(name + "/model.safetensors")),
                artifact(runtime_path, self.logical(name + "/runtime.pt")),
                artifact(state_path, self.logical(name + "/state.json")),
            ] if IS_PRIMARY else [],
        }
        self.checkpoints.append(item)
        emit(
            "event",
            event={
                "kind": "checkpoint",
                "message": f"checkpoint step {self.step_number} persisted",
                "step": self.step_number,
                "tokens": self.consumed_tokens,
                "checkpoint": item,
            },
        )

    def restore_checkpoint(self):
        if self.resume["step"] <= 0 or self.resume["step"] > self.target_steps:
            raise ValueError(f"resume step {self.resume['step']} must be in 1..{self.target_steps}")
        with open(self.resume_paths["state.json"], "r", encoding="utf-8") as stream:
            state = json.load(stream)
        backend_name = "torchtitan" if self.distributed else "pytorch"
        revision = TORCHTITAN_REVISION if self.distributed else WORKER_REVISION
        if (
            state.get("kind") != "waldo-training-checkpoint"
            or state.get("schema") != 1
            or state.get("backend") != backend_name
            or state.get("backend_revision") != revision
            or state.get("run_id") != self.begin["run_id"]
            or state.get("architecture_sha256") != self.begin["architecture_sha256"]
            or state.get("step") != self.resume["step"]
            or state.get("consumed_tokens") != self.resume["tokens"]
            or state.get("world_size") != self.world_size
        ):
            raise ValueError("PyTorch checkpoint state does not match the requested run, backend, and resume point")
        runtime = torch.load(self.resume_paths["runtime.pt"], map_location="cpu", weights_only=True)
        if self.distributed:
            from torch.distributed.checkpoint.state_dict import set_optimizer_state_dict, StateDictOptions

            set_optimizer_state_dict(
                self.model,
                self.optimizer,
                optim_state_dict=runtime["optimizer"],
                options=StateDictOptions(full_state_dict=True, cpu_offload=True),
            )
        else:
            self.optimizer.load_state_dict(runtime["optimizer"])
        random_state = runtime["random_states"][self.rank]
        torch.set_rng_state(random_state["cpu"])
        if self.device.type == "cuda" and random_state["cuda"] is not None:
            torch.cuda.set_rng_state(random_state["cuda"], self.device)
        self.step_number = self.resume["step"]
        self.consumed_tokens = self.resume["tokens"]
        self.replay_steps = self.resume["step"]
        self.checkpoints = [self.resume["checkpoint"]]

    def record_evaluation(self, _training_loss):
        if not self.evaluation_sequences:
            return
        self.model.eval()
        total_loss = 0.0
        total_tokens = 0.0
        with torch.no_grad():
            for offset in range(0, len(self.evaluation_sequences), self.batch_size):
                batch = self.evaluation_sequences[offset : offset + self.batch_size]
                tokens = torch.tensor([item[0] for item in batch], dtype=torch.long, device=self.device)
                mask = torch.tensor([item[1] for item in batch], dtype=torch.float32, device=self.device)
                logits = self.model(tokens[:, :-1])
                losses = functional.cross_entropy(logits.float().reshape(-1, logits.shape[-1]), tokens[:, 1:].reshape(-1), reduction="none")
                total_loss += float((losses.reshape_as(mask) * mask).sum().detach().cpu().item())
                total_tokens += float(mask.sum().detach().cpu().item())
        self.model.train()
        loss_value = total_loss / total_tokens
        item = {
            "step": self.step_number,
            "tokens": self.consumed_tokens,
            "metrics": {"heldout_loss": loss_value, "heldout_perplexity": math.exp(min(loss_value, 80.0))},
        }
        self.evaluations.append(item)
        emit(
            "event",
            event={
                "kind": "evaluation",
                "message": f"step {self.step_number} held-out loss {loss_value:.4f}",
                "step": self.step_number,
                "tokens": self.consumed_tokens,
                "evaluation": item,
            },
        )

    def finish(self):
        evaluation_set = self.begin["evaluation_set"]
        if self.evaluation_record_count != evaluation_set["records"] or self.evaluation_token_targets != evaluation_set["token_targets"]:
            raise ValueError(
                f"evaluation stream has {self.evaluation_record_count} records and {self.evaluation_token_targets} targets; "
                f"run BOM pins {evaluation_set['records']} records and {evaluation_set['token_targets']} targets"
            )
        if self.step_number < self.target_steps and len(self.token_buffer) > 1:
            valid_targets = min(self.sequence_length, len(self.token_buffer) - 1)
            self.add_sequence(self.token_buffer[: self.sequence_length + 1], valid_targets)
        if self.step_number < self.target_steps and self.batch:
            self.train_batch()
        if self.step_number != self.target_steps:
            raise ValueError(
                f"canonical stream produced only {self.step_number} training steps; profile requires {self.target_steps}"
            )
        if self.parameters["checkpoint_every"] > 0 and (
            not self.checkpoints or self.checkpoints[-1]["step"] != self.step_number
        ):
            self.save_checkpoint()
        if self.parameters["evaluate_every"] > 0 and (
            not self.evaluations or self.evaluations[-1]["step"] != self.step_number
        ):
            self.record_evaluation(self.final_loss)

        weights_name = "model.safetensors"
        weights_path = os.path.join(self.artifact_directory, weights_name)
        backend_name = "torchtitan" if self.distributed else "pytorch"
        self.save_weights(weights_path, f"waldo-{backend_name}-model", self.step_number)
        config_name = "config.json"
        config_path = os.path.join(self.artifact_directory, config_name)
        backend_revision = TORCHTITAN_REVISION if self.distributed else WORKER_REVISION
        if IS_PRIMARY:
            write_json(
                config_path,
                {
                    "kind": f"waldo-{backend_name}-model-config",
                    "schema": 1,
                    "architecture_sha256": self.begin["architecture_sha256"],
                    "architecture": self.architecture,
                    "training_profile": self.parameters,
                    "initialization": None if self.initialization is None else {
                        "source_type": self.initialization.get("source_type", "run"),
                        "source_id": self.initialization.get("source_id", self.initialization.get("source_run_id")),
                        "source_run_id": self.initialization.get("source_run_id"),
                        "artifact": self.initialization["artifact"],
                    },
                    "backend": {"name": backend_name, "revision": backend_revision, "version": torch.__version__, "device": str(self.device), "world_size": self.world_size},
                },
            )
            write_json(
                os.path.join(self.artifact_directory, "tokenizer.json"),
                {
                    "kind": "waldo-byte-tokenizer",
                    "schema": 1,
                    "name": "byte",
                    "revision": "builtin-byte-schema-1",
                    "pad_id": 0,
                    "bos_id": 1,
                    "eos_id": 2,
                    "byte_offset": 3,
                    "vocabulary_size": 259,
                },
            )
        tokenizer_name = "tokenizer.json"
        tokenizer_path = os.path.join(self.artifact_directory, tokenizer_name)
        if self.distributed:
            torch.distributed.barrier()
        outputs = []
        if IS_PRIMARY:
            outputs = [
                artifact(weights_path, self.logical(weights_name)),
                artifact(config_path, self.logical(config_name)),
                artifact(tokenizer_path, self.logical(tokenizer_name)),
            ]
        emit(
            "complete",
            observation={
                "simulated": False,
                "steps": self.step_number,
                "consumed_tokens": self.consumed_tokens,
                "final_loss": self.final_loss,
                "checkpoints": self.checkpoints,
                "evaluations": self.evaluations,
                "artifacts": outputs,
            },
        )


def run():
    if len(sys.argv) != 4:
        raise ValueError("worker requires artifact directory, artifact prefix, and device")
    artifact_directory = os.path.abspath(sys.argv[1])
    artifact_prefix = sys.argv[2]
    device = sys.argv[3]
    global IS_PRIMARY
    distributed = device == "torchtitan"
    if distributed:
        torch.distributed.init_process_group("nccl")
        IS_PRIMARY = torch.distributed.get_rank() == 0
    os.makedirs(artifact_directory, exist_ok=True)
    trainer = None
    ended = False
    while True:
        if distributed:
            value = [sys.stdin.readline() if IS_PRIMARY else None]
            torch.distributed.broadcast_object_list(value, src=0)
            line = value[0]
        else:
            line = sys.stdin.readline()
        if not line:
            break
        frame = json.loads(line)
        if frame.get("schema") != PROTOCOL_SCHEMA:
            raise ValueError(f"unsupported worker input schema {frame.get('schema')}")
        kind = frame.get("kind")
        if kind == "begin":
            if trainer is not None:
                raise ValueError("worker received duplicate begin frame")
            trainer = Trainer(frame["begin"], artifact_directory, artifact_prefix, device)
        elif kind == "record":
            if trainer is None or ended:
                raise ValueError("worker received record outside stream")
            trainer.add_record(frame["record"])
        elif kind == "evaluation_record":
            if trainer is None or ended:
                raise ValueError("worker received evaluation record outside stream")
            trainer.add_evaluation_record(frame["record"])
        elif kind == "end":
            if trainer is None or ended:
                raise ValueError("worker received invalid end frame")
            ended = True
        else:
            raise ValueError(f"unsupported worker input kind {kind!r}")
    if trainer is None or not ended:
        raise ValueError("worker input ended without begin/end framing")
    trainer.finish()


try:
    run()
except Exception as error:
    traceback.print_exc(file=sys.stderr)
    emit("error", error=str(error))
    sys.exit(1)
finally:
    if torch.distributed.is_initialized():
        torch.distributed.destroy_process_group()
