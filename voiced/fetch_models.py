"""Chạy lúc build image (trước khi chép voiced.py, để sửa code không phải tải lại mô hình).
Mô hình nằm sẵn trong image dưới dạng file thật (không symlink cache HF) để voiced chạy khi mất internet."""
import hashlib
import urllib.request
from pathlib import Path

import sentencepiece as spm
from huggingface_hub import hf_hub_download
from vieneu._v3_turbo_engine import onnx_runtime_lite as orl

MODELS = Path("/models")
STT_REPO, STT_REV = "g-group-ai-lab/gipformer1.5-68M-rnnt", "dd9227dcd8705c13f33bdbe59728d546ab94480f"  # 68M, MIT; đo: mục 15.2.1
TTS_REPO, TTS_REV = "pnnbao-ump/VieNeu-TTS-v3-Turbo", "61b85e3d937fbbacb387714180e8182823512523"
CODEC_REV = "ceff0d0749bfb3fa2d61149794ec6feef0d1e1ae"
# Nhận người nói: CAM++ zh_en 16k common advanced (3D-Speaker, ModelScope iic/speech_campplus_sv_zh_en_16k-common_advanced,
# Apache License 2.0), bản ONNX của sherpa-onnx, 28 MB. Đo: mục 15.2.4.
SPEAKER_URL = ("https://github.com/k2-fsa/sherpa-onnx/releases/download/speaker-recongition-models/"
               "3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx")
SPEAKER_SHA256 = "aa3cfc16963a10586a9393f5035d6d6b57e98d358b347f80c2a30bf4f00ceba2"


def main():
    stt = MODELS / "stt"
    for f in ("encoder.int8.onnx", "decoder.onnx", "joiner.int8.onnx", "tokens.txt", "bpe.model"):
        hf_hub_download(STT_REPO, f, revision=STT_REV, local_dir=stt)
    # bpe.vocab ("mảnh<TAB>điểm" mỗi dòng): sherpa-onnx cần để tách từ khoá ưu tiên thành các mảnh BPE của mô hình
    sp = spm.SentencePieceProcessor(model_file=str(stt / "bpe.model"))
    (stt / "bpe.vocab").write_text("".join(f"{sp.id_to_piece(i)}\t{sp.get_score(i)}\n" for i in range(sp.get_piece_size())))
    for f in orl._GRAPH_FILES:  # bản int8: nhỏ và nhanh hơn fp32 trên CPU (đo trên máy Ubuntu)
        hf_hub_download(TTS_REPO, f, subfolder="onnx_int8", revision=TTS_REV, local_dir=MODELS / "vieneu")
    for f in orl._CODEC_FILES:
        hf_hub_download(orl._CODEC_REPO, f, revision=CODEC_REV, local_dir=MODELS / "codec")
    data = urllib.request.urlopen(SPEAKER_URL, timeout=120).read()
    if hashlib.sha256(data).hexdigest() != SPEAKER_SHA256:
        raise SystemExit("mô hình nhận người nói tải về sai SHA-256")
    (MODELS / "speaker").mkdir(parents=True, exist_ok=True)
    (MODELS / "speaker" / "campplus.onnx").write_bytes(data)


if __name__ == "__main__":
    main()
