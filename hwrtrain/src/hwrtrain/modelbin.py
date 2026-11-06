"""CNN definition + model.bin (HWR1) writer/reader.

The binary format is documented in backend/hwr/model.go. read_model() builds
a plain float PyTorch network from the *dequantised* int8 weights, i.e.
exactly what the Go engine computes; that network is the reference for the
Go/PyTorch parity file and the starting point for per-user fine-tuning.

Architecture spec: dash-separated tokens, e.g. "c16-p-c32-p-c64-p-f128"
  cN  3x3 conv, pad 1, N channels, BatchNorm (training only), ReLU
  p   2x2 max-pool stride 2
  fN  dense N units + ReLU (+ dropout while training)
The final dense layer to len(LABELS) outputs is implicit.
"""

from __future__ import annotations

import struct
import zlib

import numpy as np
import torch
from torch import nn

# Output classes: digits 0-9 plus an explicit reject class. '0' is a real
# class (so 0-like shapes do not get forced into 6/9) but sudoku has no 0,
# so Go maps both '0' and '?' to Unknown.
LABELS = b"0123456789?"
REJECT = 10
NCLASS = len(LABELS)


def make_net(arch: str, nclass: int = NCLASS, dropout: float = 0.25) -> nn.Sequential:
    layers: list[nn.Module] = []
    c, h = 1, 28
    flat = None
    for tok in arch.split("-"):
        if tok[0] == "c":
            n = int(tok[1:])
            layers += [nn.Conv2d(c, n, 3, padding=1, bias=False), nn.BatchNorm2d(n), nn.ReLU()]
            c = n
        elif tok == "p":
            layers.append(nn.MaxPool2d(2))
            h //= 2
        elif tok[0] == "f":
            n = int(tok[1:])
            if flat is None:
                layers.append(nn.Flatten())
                flat = c * h * h
            layers += [nn.Linear(flat, n), nn.ReLU(), nn.Dropout(dropout)]
            flat = n
        else:
            raise ValueError(tok)
    if flat is None:
        layers.append(nn.Flatten())
        flat = c * h * h
    layers += [nn.Dropout(dropout), nn.Linear(flat, nclass)]
    return nn.Sequential(*layers)


def macs(arch: str) -> int:
    c, h, tot, flat = 1, 28, 0, None
    for tok in arch.split("-"):
        if tok[0] == "c":
            n = int(tok[1:])
            tot += h * h * n * c * 9
            c = n
        elif tok == "p":
            h //= 2
        elif tok[0] == "f":
            n = int(tok[1:])
            flat = flat or c * h * h
            tot += flat * n
            flat = n
    flat = flat or c * h * h
    return tot + flat * NCLASS


def _fold(net: nn.Sequential):
    """Yield ('conv'|'pool'|'dense', params) with BN folded into conv."""
    mods = [m for m in net if not isinstance(m, (nn.Dropout, nn.Flatten))]
    i = 0
    while i < len(mods):
        m = mods[i]
        if isinstance(m, nn.Conv2d):
            w = m.weight.detach().double()
            b = m.bias.detach().double() if m.bias is not None else torch.zeros(w.shape[0], dtype=torch.float64)
            relu = False
            j = i + 1
            if j < len(mods) and isinstance(mods[j], nn.BatchNorm2d):
                bn = mods[j]
                g = bn.weight.detach().double() / torch.sqrt(bn.running_var.double() + bn.eps)
                w = w * g[:, None, None, None]
                b = (b - bn.running_mean.double()) * g + bn.bias.detach().double()
                j += 1
            if j < len(mods) and isinstance(mods[j], nn.ReLU):
                relu = True
                j += 1
            yield "conv", dict(w=w, b=b, k=m.kernel_size[0], stride=m.stride[0], pad=m.padding[0], relu=relu)
            i = j
        elif isinstance(m, nn.MaxPool2d):
            k = m.kernel_size if isinstance(m.kernel_size, int) else m.kernel_size[0]
            s = m.stride if isinstance(m.stride, int) else m.stride[0]
            yield "pool", dict(k=k, stride=s)
            i += 1
        elif isinstance(m, nn.Linear):
            relu = i + 1 < len(mods) and isinstance(mods[i + 1], nn.ReLU)
            yield "dense", dict(w=m.weight.detach().double(), b=m.bias.detach().double(), relu=relu)
            i += 2 if relu else 1
        else:
            raise TypeError(type(m))


def _quant(w: torch.Tensor):
    """Symmetric per-output-channel int8."""
    flat = w.reshape(w.shape[0], -1)
    amax = flat.abs().max(dim=1).values.clamp_min(1e-12)
    scale = (amax / 127.0).float()
    q = torch.round(flat / scale.double()[:, None]).clamp(-127, 127).to(torch.int8)
    return q, scale


def write_model(net: nn.Sequential, path, threshold: float, labels: bytes = LABELS) -> bytes:
    net = net.eval().cpu()
    layers = list(_fold(net))
    out = bytearray(b"HWR1")
    out += struct.pack("<HH", 1, len(labels)) + labels
    out += struct.pack("<f", threshold)
    out += struct.pack("<HHH", 1, 28, 28)
    out += struct.pack("<H", len(layers))
    for kind, p in layers:
        if kind == "conv":
            q, s = _quant(p["w"])
            oc, ic = p["w"].shape[:2]
            out += struct.pack("<BHHBBBB", 1, oc, ic, p["k"], p["stride"], p["pad"], int(p["relu"]))
            out += s.numpy().astype("<f4").tobytes() + p["b"].float().numpy().astype("<f4").tobytes()
            out += q.numpy().tobytes()
        elif kind == "pool":
            out += struct.pack("<BBB", 2, p["k"], p["stride"])
        else:
            q, s = _quant(p["w"])
            o, i = p["w"].shape
            out += struct.pack("<BHHB", 3, o, i, int(p["relu"]))
            out += s.numpy().astype("<f4").tobytes() + p["b"].float().numpy().astype("<f4").tobytes()
            out += q.numpy().tobytes()
    out += struct.pack("<I", zlib.crc32(bytes(out)) & 0xFFFFFFFF)
    blob = bytes(out)
    if path is not None:
        with open(path, "wb") as f:
            f.write(blob)
    return blob


class ModelFile:
    def __init__(self, net, labels, threshold, crc):
        self.net, self.labels, self.threshold, self.crc = net, labels, threshold, crc


def read_model(path_or_bytes) -> ModelFile:
    """Rebuild the dequantised float network (no BN, no dropout)."""
    b = path_or_bytes if isinstance(path_or_bytes, (bytes, bytearray)) else open(path_or_bytes, "rb").read()
    assert b[:4] == b"HWR1", "not HWR1"
    crc = struct.unpack_from("<I", b, len(b) - 4)[0]
    assert zlib.crc32(b[:-4]) & 0xFFFFFFFF == crc, "crc mismatch"
    off = 4
    ver, nl = struct.unpack_from("<HH", b, off); off += 4
    assert ver == 1
    labels = bytes(b[off:off + nl]); off += nl
    (thr,) = struct.unpack_from("<f", b, off); off += 4
    off += 6
    (nlayer,) = struct.unpack_from("<H", b, off); off += 2
    mods: list[nn.Module] = []
    flat = False

    def f32(n):
        nonlocal off
        a = np.frombuffer(b, "<f4", n, off).copy(); off += 4 * n
        return a

    def i8(n):
        nonlocal off
        a = np.frombuffer(b, np.int8, n, off).astype(np.float32); off += n
        return a

    for _ in range(nlayer):
        op = b[off]; off += 1
        if op == 1:
            oc, ic, k, s, p, relu = struct.unpack_from("<HHBBBB", b, off); off += 8
            sc, bias = f32(oc), f32(oc)
            w = i8(oc * ic * k * k).reshape(oc, ic * k * k) * sc[:, None]
            conv = nn.Conv2d(ic, oc, k, stride=s, padding=p)
            conv.weight.data = torch.from_numpy(w.reshape(oc, ic, k, k).astype(np.float32))
            conv.bias.data = torch.from_numpy(bias)
            mods.append(conv)
            if relu:
                mods.append(nn.ReLU())
        elif op == 2:
            k, s = struct.unpack_from("<BB", b, off); off += 2
            mods.append(nn.MaxPool2d(k, s))
        elif op == 3:
            o, i, relu = struct.unpack_from("<HHB", b, off); off += 5
            sc, bias = f32(o), f32(o)
            w = i8(o * i).reshape(o, i) * sc[:, None]
            if not flat:
                mods.append(nn.Flatten()); flat = True
            lin = nn.Linear(i, o)
            lin.weight.data = torch.from_numpy(w.astype(np.float32))
            lin.bias.data = torch.from_numpy(bias)
            mods.append(lin)
            if relu:
                mods.append(nn.ReLU())
        else:
            raise ValueError(op)
    assert off == len(b) - 4
    return ModelFile(nn.Sequential(*mods).eval(), labels, thr, crc)
