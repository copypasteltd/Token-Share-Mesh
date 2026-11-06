"""Approved PNG, fragmented MP4, PCM WAV and geometry-only GLB writers.

All encoders write directly to CipherSpool. They never create plaintext media,
carry EXIF/prompt metadata, call a subprocess or build a second whole video.
"""
from __future__ import annotations

from fractions import Fraction
import math
import struct
import wave

from .limits import MAX_MEDIA, Rejected, canonical, integer


def _pixels(tensor, cancelled, maximum_pixels=1048576):
    import numpy as np
    integer(len(tensor.shape), 3, 3)
    height, width, channels = map(int, tensor.shape)
    integer(width, 1, 2048)
    integer(height, 1, 2048)
    if channels not in (3, 4) or width * height > maximum_pixels:
        raise Rejected('LIMIT')
    out = np.empty((height, width, channels), dtype=np.uint8)
    try:
        # Bounded row conversion; never copy a complete floating-point frame
        # or video to the CPU just to quantize it.
        for row in range(0, height, 16):
            if cancelled():
                raise Rejected('CANCELLED')
            part = tensor[row:row + 16].detach().to(device='cpu', dtype=__import__('torch').float32).numpy()
            if not np.isfinite(part).all():
                raise Rejected('FORMAT')
            out[row:row + 16] = np.rint(np.clip(part, 0, 1) * 255).astype(np.uint8)
        return out
    except BaseException:
        out.fill(0)
        raise


def png(images, sink, cancelled):
    from PIL import Image
    if len(images.shape) != 4 or int(images.shape[0]) != 1 or cancelled():
        raise Rejected('FORMAT')
    pixels = _pixels(images[0], cancelled)
    try:
        with Image.fromarray(pixels) as image:
            image.info.clear()
            image.save(sink, format='PNG', compress_level=3)
        if cancelled():
            raise Rejected('CANCELLED')
    finally:
        pixels.fill(0)


def mp4(images, sink, cancelled, fps):
    import av
    fps = integer(fps, 1, 60)
    if len(images.shape) != 4:
        raise Rejected('FORMAT')
    count, height, width, channels = map(int, images.shape)
    integer(count, 1, 240)
    integer(width, 2, 2048)
    integer(height, 2, 2048)
    if width % 2 or height % 2 or channels != 3 or count / fps > 30:
        raise Rejected('LIMIT')
    if width * height > 1048576:
        raise Rejected('LIMIT')
    # The approved PyAV/FFmpeg build must contain this exact encoder. Missing
    # libx264 is rejection, never a subprocess or a different encoding profile.
    with av.open(sink, mode='w', format='mp4', options={
        'movflags': 'frag_keyframe+empty_moov+default_base_moof',
    }) as container:
        stream = container.add_stream('libx264', rate=Fraction(fps, 1))
        stream.width, stream.height = width, height
        stream.pix_fmt = 'yuv420p'
        stream.options = {'preset': 'ultrafast', 'crf': '20', 'threads': '1'}
        for index in range(count):
            if cancelled():
                raise Rejected('CANCELLED')
            pixels = _pixels(images[index], cancelled)
            try:
                frame = av.VideoFrame.from_ndarray(pixels, format='rgb24')
                frame.pts = index
                frame.time_base = Fraction(1, fps)
                for packet in stream.encode(frame):
                    container.mux(packet)
            finally:
                pixels.fill(0)
        for packet in stream.encode(None):
            if cancelled():
                raise Rejected('CANCELLED')
            container.mux(packet)


def wav(audio, sink, cancelled):
    import numpy as np
    import torch
    if type(audio) is not dict or set(audio) != {'waveform', 'sample_rate'}:
        raise Rejected('FORMAT')
    rate = integer(audio['sample_rate'], 8000, 96000)
    samples = audio['waveform']
    if len(samples.shape) != 3:
        raise Rejected('FORMAT')
    batch, channels, frames = map(int, samples.shape)
    if batch != 1 or channels not in (1, 2) or not 1 <= frames <= rate * 120:
        raise Rejected('LIMIT')
    if frames * channels * 2 + 44 > MAX_MEDIA:
        raise Rejected('LIMIT')
    with wave.open(sink, 'wb') as target:
        target.setnchannels(channels)
        target.setsampwidth(2)
        target.setframerate(rate)
        target.setnframes(frames)
        for offset in range(0, frames, 4096):
            if cancelled():
                raise Rejected('CANCELLED')
            part = samples[0, :, offset:offset + 4096].detach().to(device='cpu', dtype=torch.float32).numpy()
            if not np.isfinite(part).all():
                raise Rejected('FORMAT')
            quantized = np.rint(np.clip(part, -1, 1) * 32767).astype('<i2')
            target.writeframesraw(quantized.T.copy(order='C').tobytes())
            quantized.fill(0)
        # Known exact nframes prevents wave from seeking back into ciphertext.


def glb(mesh, sink, cancelled):
    import numpy as np
    vertices, faces = mesh.vertices, mesh.faces
    if vertices.ndim != 2 or vertices.shape[1] != 3 or faces.ndim != 2 or faces.shape[1] != 3:
        raise Rejected('FORMAT')
    vertex_count = integer(int(vertices.shape[0]), 3, 500000)
    face_count = integer(int(faces.shape[0]), 1, 1000000)
    if faces.dtype.kind not in 'iu' or vertices.dtype.kind not in 'fiu':
        raise Rejected('FORMAT')
    minimum = np.full(3, math.inf)
    maximum = np.full(3, -math.inf)
    for offset in range(0, vertex_count, 4096):
        if cancelled():
            raise Rejected('CANCELLED')
        part = np.asarray(vertices[offset:offset + 4096], dtype='<f4')
        if not np.isfinite(part).all():
            raise Rejected('FORMAT')
        minimum = np.minimum(minimum, part.min(axis=0))
        maximum = np.maximum(maximum, part.max(axis=0))
    for offset in range(0, face_count, 4096):
        if cancelled():
            raise Rejected('CANCELLED')
        part = faces[offset:offset + 4096]
        if part.min() < 0 or part.max() >= vertex_count:
            raise Rejected('FORMAT')
    vertex_bytes, index_bytes = vertex_count * 12, face_count * 12
    document = {
        'asset': {'version': '2.0', 'generator': 'Token Share Mesh private geometry encoder'},
        'scene': 0, 'scenes': [{'nodes': [0]}], 'nodes': [{'mesh': 0}],
        'meshes': [{'primitives': [{'attributes': {'POSITION': 0}, 'indices': 1, 'mode': 4}]}],
        'buffers': [{'byteLength': vertex_bytes + index_bytes}],
        'bufferViews': [
            {'buffer': 0, 'byteOffset': 0, 'byteLength': vertex_bytes, 'target': 34962},
            {'buffer': 0, 'byteOffset': vertex_bytes, 'byteLength': index_bytes, 'target': 34963}],
        'accessors': [
            {'bufferView': 0, 'componentType': 5126, 'count': vertex_count, 'type': 'VEC3',
             'min': minimum.tolist(), 'max': maximum.tolist()},
            {'bufferView': 1, 'componentType': 5125, 'count': face_count * 3, 'type': 'SCALAR'}],
    }
    meta = canonical(document)
    meta += b' ' * (-len(meta) % 4)
    size = 12 + 8 + len(meta) + 8 + vertex_bytes + index_bytes
    if size > MAX_MEDIA:
        raise Rejected('LIMIT')
    sink.write(struct.pack('<4sII', b'glTF', 2, size))
    sink.write(struct.pack('<I4s', len(meta), b'JSON'))
    sink.write(meta)
    sink.write(struct.pack('<I4s', vertex_bytes + index_bytes, b'BIN\0'))
    for source, count, dtype in ((vertices, vertex_count, '<f4'), (faces, face_count, '<u4')):
        for offset in range(0, count, 4096):
            if cancelled():
                raise Rejected('CANCELLED')
            sink.write(np.asarray(source[offset:offset + 4096], dtype=dtype, order='C').tobytes())
