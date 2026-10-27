"""Chạy lúc build image (trước khi chép voiced.py, để sửa code không phải tải lại mô hình).
Mô hình nằm sẵn trong image dưới dạng file thật (không symlink cache HF) để voiced chạy khi mất internet."""
import io
import tarfile
import urllib.request
from pathlib import Path

from huggingface_hub import hf_hub_download
from vieneu._v3_turbo_engine import onnx_runtime_lite as orl

MODELS = Path("/models")
STT_MODEL = "sherpa-onnx-zipformer-vi-30M-int8-2026-02-09"  # hynt/Zipformer-30M-RNNT-6000h, đo: mục 15.2.1 thiết kế
TTS_REPO, TTS_REV = "pnnbao-ump/VieNeu-TTS-v3-Turbo", "61b85e3d937fbbacb387714180e8182823512523"
CODEC_REV = "ceff0d0749bfb3fa2d61149794ec6feef0d1e1ae"


def main():
    (MODELS / "stt").mkdir(parents=True, exist_ok=True)
    url = f"https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/{STT_MODEL}.tar.bz2"
    with urllib.request.urlopen(url) as r, tarfile.open(fileobj=io.BytesIO(r.read()), mode="r:bz2") as t:
        for m in t.getmembers():
            if Path(m.name).name in {"encoder.int8.onnx", "decoder.onnx", "joiner.int8.onnx", "tokens.txt"}:
                (MODELS / "stt" / Path(m.name).name).write_bytes(t.extractfile(m).read())
    for f in orl._GRAPH_FILES:  # bản int8: nhỏ và nhanh hơn fp32 trên CPU (đo trên máy Ubuntu)
        hf_hub_download(TTS_REPO, f, subfolder="onnx_int8", revision=TTS_REV, local_dir=MODELS / "vieneu")
    for f in orl._CODEC_FILES:
        hf_hub_download(orl._CODEC_REPO, f, revision=CODEC_REV, local_dir=MODELS / "codec")


if __name__ == "__main__":
    main()
