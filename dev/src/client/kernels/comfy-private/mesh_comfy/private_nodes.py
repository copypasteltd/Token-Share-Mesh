"""Private file-like input and metadata-free output nodes for fixed graphs."""
from __future__ import annotations

import threading

from . import encoders
from .limits import Rejected

_local = threading.local()


def enter(context):
    if getattr(_local, 'job', None) is not None:
        raise Rejected('UNKNOWN')
    _local.job = context


def leave():
    _local.job = None


def current():
    job = getattr(_local, 'job', None)
    if job is None or job.cancelled():
        raise Rejected('CANCELLED')
    return job


class ImageInput:
    RETURN_TYPES = ('IMAGE',)
    FUNCTION = 'load'
    CATEGORY = 'mesh/private'

    @classmethod
    def INPUT_TYPES(cls):
        return {'required': {'asset_id': ('STRING',)}}

    def load(self, asset_id):
        from PIL import Image
        import numpy as np
        import torch
        job = current()
        asset = job.assets.get(asset_id)
        if asset is None:
            raise Rejected('RECIPE')
        with asset.reader() as source:
            # Pillow's WebP decoder reads the entire source. This bounded
            # profile deliberately accepts only PNG/JPEG input; no read-all
            # exception or plaintext temporary compatibility path is added.
            with Image.open(source, formats=['PNG', 'JPEG']) as image:
                if image.width * image.height > 1048576 or getattr(image, 'n_frames', 1) != 1:
                    raise Rejected('LIMIT')
                with image.convert('RGB') as rgb:
                    pixels = np.array(rgb, dtype=np.uint8)
                    try:
                        tensor = torch.from_numpy(pixels).to(dtype=torch.float32)
                        tensor.div_(255.0)
                        return (tensor.unsqueeze(0),)
                    finally:
                        pixels.fill(0)


class _Sink:
    RETURN_TYPES = ()
    FUNCTION = 'save'
    OUTPUT_NODE = True
    CATEGORY = 'mesh/private'

    def output(self, kind, value, **options):
        job = current()
        if kind != job.recipe.kind or job.output is not None:
            raise Rejected('RECIPE')
        sink = job.workspace.spool(job.scope, job.recipe.maximum)
        job.output = sink
        getattr(encoders, kind.lower())(value, sink, job.cancelled, **options)
        sink.seal()
        # No filenames, previews, raw bytes, graph or prompt are put in ui.
        return ()


class PNG(_Sink):
    @classmethod
    def INPUT_TYPES(cls):
        return {'required': {'images': ('IMAGE',)}}

    def save(self, images):
        return self.output('PNG', images)


class MP4(_Sink):
    @classmethod
    def INPUT_TYPES(cls):
        return {'required': {'images': ('IMAGE',), 'fps': ('INT', {'default': 24, 'min': 1, 'max': 60})}}

    def save(self, images, fps):
        return self.output('MP4', images, fps=fps)


class WAV(_Sink):
    @classmethod
    def INPUT_TYPES(cls):
        return {'required': {'audio': ('AUDIO',)}}

    def save(self, audio):
        return self.output('WAV', audio)


class GLB(_Sink):
    @classmethod
    def INPUT_TYPES(cls):
        return {'required': {'mesh': ('MESH',)}}

    def save(self, mesh):
        return self.output('GLB', mesh)


NODE_CLASS_MAPPINGS = {'MeshPrivateImage': ImageInput, 'MeshPrivatePNG': PNG,
                       'MeshPrivateMP4': MP4, 'MeshPrivateWAV': WAV,
                       'MeshPrivateGLB': GLB}
