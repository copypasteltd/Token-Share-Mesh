"""SOURCE_ONLY media codec definitions for the future pinned runtime batch.

Synthetic CPU tensors verify the encoders, not inference, approved GPU binding,
node packs, actual models, remote qualification or memory/performance budgets.
"""
from contextlib import contextmanager
import io
import json
import struct
from types import SimpleNamespace
import unittest
import wave

from test_support import owned_directory, scope
from mesh_comfy import encoders
from mesh_comfy.limits import Rejected
from mesh_comfy.spool import CipherSpool


class EncoderBoundary(unittest.TestCase):
    @contextmanager
    def target(self):
        with owned_directory() as directory:
            spool = CipherSpool(directory, scope(), 1 << 20, lambda _: None, lambda: False)
            try:
                yield spool
            finally:
                spool.close()

    def encoded(self, spool):
        spool.seal()
        with spool.reader() as reader:
            return reader.read(1 << 20)

    def test_png_real_codec_roundtrip_contains_pixels_without_prompt_metadata(self):
        import torch
        from PIL import Image
        with self.target() as spool:
            images = torch.zeros((1, 2, 2, 3));images[:, :, :, 0] = 1
            encoders.png(images, spool, lambda: False)
            with Image.open(io.BytesIO(self.encoded(spool))) as image:
                self.assertEqual(image.size, (2, 2))
                self.assertEqual(image.getpixel((0, 0)), (255, 0, 0))
                self.assertEqual(image.info, {})

    def test_png_refuses_multiple_outputs_and_nonfinite_pixels(self):
        import torch
        for images in (torch.zeros((2, 2, 2, 3)), torch.full((1, 2, 2, 3), float('nan'))):
            with self.target() as spool:
                with self.assertRaises(Rejected):encoders.png(images, spool, lambda: False)

    def test_fragmented_mp4_real_codec_roundtrip_has_exact_frame_count(self):
        import av
        import torch
        with self.target() as spool:
            images = torch.zeros((4, 32, 32, 3));images[2:, :, :, 1] = 1
            encoders.mp4(images, spool, lambda: False, fps=2)
            data = self.encoded(spool)
            self.assertIn(b'moof', data)
            with av.open(io.BytesIO(data), mode='r', format='mp4') as container:
                frames = list(container.decode(video=0))
                self.assertEqual(len(frames), 4)
                self.assertEqual((frames[0].width, frames[0].height), (32, 32))
                self.assertNotIn('prompt', container.metadata)

    def test_mp4_limits_do_not_silently_resize_or_select_another_codec(self):
        import torch
        for images, fps in ((torch.zeros((1, 31, 32, 3)), 2),
                            (torch.zeros((1, 32, 32, 4)), 2),
                            (torch.zeros((1, 32, 32, 3)), 0)):
            with self.target() as spool:
                with self.assertRaises(Rejected):encoders.mp4(images, spool, lambda: False, fps=fps)

    def test_pcm_wav_real_codec_roundtrip_uses_exact_known_frames(self):
        import torch
        with self.target() as spool:
            samples = torch.zeros((1, 2, 16));samples[0, 0, 0] = 1;samples[0, 1, 0] = -1
            encoders.wav({'waveform': samples, 'sample_rate': 8000}, spool, lambda: False)
            with wave.open(io.BytesIO(self.encoded(spool)), 'rb') as source:
                self.assertEqual((source.getnchannels(), source.getsampwidth(), source.getframerate(), source.getnframes()), (2, 2, 8000, 16))
                self.assertEqual(struct.unpack('<hh', source.readframes(1)), (32767, -32767))

    def test_glb_geometry_roundtrip_has_bounded_positions_and_indices(self):
        import numpy as np
        mesh = SimpleNamespace(vertices=np.array([[0, 0, 0], [1, 0, 0], [0, 1, 0]], dtype=np.float32),
                               faces=np.array([[0, 1, 2]], dtype=np.int64))
        with self.target() as spool:
            encoders.glb(mesh, spool, lambda: False)
            data = self.encoded(spool)
            self.assertEqual(struct.unpack('<4sII', data[:12]), (b'glTF', 2, len(data)))
            length, kind = struct.unpack('<I4s', data[12:20]);self.assertEqual(kind, b'JSON')
            meta = json.loads(data[20:20 + length])
            self.assertEqual(meta['accessors'][0]['count'], 3)
            self.assertEqual(meta['accessors'][1]['count'], 3)
            self.assertNotIn('extras', meta)
            self.assertEqual(struct.unpack('<III', data[-12:]), (0, 1, 2))

    def test_glb_refuses_bad_indices_and_encoder_cancellation(self):
        import numpy as np
        import torch
        mesh = SimpleNamespace(vertices=np.zeros((3, 3), dtype=np.float32), faces=np.array([[0, 1, 3]]))
        with self.target() as spool:
            with self.assertRaises(Rejected):encoders.glb(mesh, spool, lambda: False)
        with self.target() as spool:
            with self.assertRaises(Rejected):encoders.png(torch.zeros((1, 2, 2, 3)), spool, lambda: True)
        with self.target() as spool:
            with self.assertRaises(Rejected):encoders.wav({'waveform': torch.zeros((1, 1, 16)), 'sample_rate': 8000}, spool, lambda: True)
