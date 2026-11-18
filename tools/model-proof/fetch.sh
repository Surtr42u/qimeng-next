#!/usr/bin/env bash
set -euo pipefail
cd "$1"
model_revision=bc640142c66e1fdd12af0bd68f40445458f3869b
model_name=Qwen3-4B-Q4_K_M.gguf
engine=llama-b12521-bin-ubuntu-x64.tar.gz
curl --fail --location --retry 3 --output "$model_name" "https://huggingface.co/Qwen/Qwen3-4B-GGUF/resolve/$model_revision/$model_name"
curl --fail --location --retry 3 --output "$engine" "https://github.com/ggml-org/llama.cpp/releases/download/b12521/$engine"
sha256sum --check <<'HASHES'
7485fe6f11af29433bc51cab58009521f205840f5b4ae3a32fa7f92e8534fdf5  Qwen3-4B-Q4_K_M.gguf
ab8343cb39e17a18f183ce0a35543a6c3dcc15fb287f55909dd182cb975e345e  llama-b12521-bin-ubuntu-x64.tar.gz
HASHES
mkdir engine
tar -xzf "$engine" -C engine
