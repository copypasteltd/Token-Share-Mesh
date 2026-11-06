"""Helpers for future batch tests; no worker, engine, device or model launch."""
from pathlib import Path
import sys
import tempfile

BRIDGE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BRIDGE))
PROJECT = Path(__file__).resolve().parents[6]


def owned_directory():
    root = PROJECT / 'runtime' / 'tests' / 'comfy-private'
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    return tempfile.TemporaryDirectory(prefix='source-case-', dir=root)


def scope():
    return {'session_id': 'session_original', 'invocation_id': 'invoke_original',
            'local_generation': '19', 'environment_digest': 'a' * 64,
            'recipe_digest': 'b' * 64}
