"""SOURCE_ONLY model metadata pins; synthetic bytes, never real inference."""
import builtins
import hashlib
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from test_support import owned_directory
from mesh_comfy.environment import Environment
from mesh_comfy.limits import Rejected


class ModelInventoryBoundary(unittest.TestCase):
    def test_model_pins_are_checked_without_importing_inference_packages(self):
        with owned_directory() as root:
            path = Path(root) / 'synthetic.safetensors'
            raw = b'NOT_A_REAL_MODEL'
            path.write_bytes(raw);path.chmod(0o600)
            record = {'category': 'vae', 'alias': 'approved.safetensors',
                      'path': str(path), 'bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}
            env = Environment.__new__(Environment);env.cancelled = lambda: False
            original_import = builtins.__import__

            def no_inference(name, *args, **kwargs):
                if name == 'torch' or name == 'nodes' or name.startswith('comfy'):
                    self.fail('model pins imported an inference package')
                return original_import(name, *args, **kwargs)

            with patch('builtins.__import__', side_effect=no_inference):
                inventory = env._model_inventory([record])
            paths = SimpleNamespace();env._models(paths, inventory)
            self.assertEqual(paths.get_full_path('vae', 'approved.safetensors'), str(path))
            self.assertIsNone(paths.get_full_path('vae', 'unlisted.safetensors'))

    def test_damaged_models_and_path_aliases_never_gain_runtime_mapping(self):
        with owned_directory() as root:
            path = Path(root) / 'synthetic.safetensors'
            path.write_bytes(b'TAMPERED');path.chmod(0o600)
            env = Environment.__new__(Environment);env.cancelled = lambda: False
            base = {'category': 'vae', 'alias': 'approved.safetensors',
                    'path': str(path), 'bytes': 8, 'sha256': 'a' * 64}
            for record in (base, dict(base, alias='../foreign'), dict(base, alias='..'), dict(base, category='arbitrary')):
                with self.assertRaises(Rejected):env._model_inventory([record])
