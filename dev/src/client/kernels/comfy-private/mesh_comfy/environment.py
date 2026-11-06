"""Fixed local package/model checks and explicit NVIDIA CUDA selection.

The original host independently verifies its signed catalog, local consent,
physical hold, grant and qualification. This private initialization cannot
create those authorities; it accepts no keys, models or approvals from buyers.
"""
from __future__ import annotations

import asyncio
import ctypes
import hashlib
import os
from pathlib import Path, PurePosixPath
import re
import sys

from . import PROFILE, UPSTREAM_COMMIT
from .io_guard import loop_bootstrap
from .limits import Rejected, canonical, digest, fields, hash_file, integer
from .limits import identifier, private_directory, regular_file
from .private_nodes import NODE_CLASS_MAPPINGS
from .recipe import DENIED_CLASSES, Recipe

_PART = re.compile(r'^[A-Za-z0-9_.][A-Za-z0-9_.+-]{0,191}$')
_UUID = re.compile(r'^GPU-[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$')
_PCI = re.compile(r'^[a-f0-9]{8}:[a-f0-9]{2}:[a-f0-9]{2}\.[0-7]$')


def relative(value):
    if type(value) is not str or not 0 < len(value) <= 1024 or '\\' in value:
        raise Rejected('ENVIRONMENT')
    path = PurePosixPath(value)
    if path.is_absolute() or str(path) != value or any(p in {'.', '..'} or _PART.fullmatch(p) is None for p in path.parts):
        raise Rejected('ENVIRONMENT')
    return value


def verify_tree(root, files, cancelled):
    if type(files) is not list or not 1 <= len(files) <= 200002:
        raise Rejected('LIMIT')
    wanted, directories, spellings = {}, set(), {}
    total = 0
    for file in files:
        fields(file, {'path', 'sha256', 'bytes'})
        name = relative(file['path'])
        digest(file['sha256'])
        integer(file['bytes'], 0, 200 << 30)
        if name in wanted or file['bytes'] > (200 << 30) - total:
            raise Rejected('ENVIRONMENT')
        total += file['bytes']
        wanted[name] = file
        for part in (PurePosixPath(name), *PurePosixPath(name).parents):
            key = str(part)
            if key == '.':
                continue
            if key.lower() in spellings and spellings[key.lower()] != key:
                raise Rejected('ENVIRONMENT')
            spellings[key.lower()] = key
            if key != name:
                directories.add(key)
    if set(wanted) & directories:
        raise Rejected('ENVIRONMENT')
    seen = set()
    for parent, dirs, names in os.walk(root, followlinks=False):
        if cancelled():
            raise Rejected('CANCELLED')
        base = Path(parent)
        private_directory(str(base))
        for name in dirs:
            path = base / name
            rel = path.relative_to(root).as_posix()
            private_directory(str(path))
            if rel not in directories:
                raise Rejected('ENVIRONMENT')
        for name in names:
            path = base / name
            rel = path.relative_to(root).as_posix()
            if rel not in wanted:
                raise Rejected('ENVIRONMENT')
            hash_file(path, wanted[rel]['bytes'], wanted[rel]['sha256'], cancelled)
            seen.add(rel)
    if seen != set(wanted):
        raise Rejected('ENVIRONMENT')
    return wanted


def cuda_identity(driver_path, uuid, pci):
    if type(uuid) is not str or _UUID.fullmatch(uuid) is None or type(pci) is not str or _PCI.fullmatch(pci) is None:
        raise Rejected('GPU')
    # An exact UUID mask is set before importing Torch/Comfy. No CPU, MPS,
    # other ordinal, GPU alias, model or backend is selected as a fallback.
    if os.environ.get('CUDA_VISIBLE_DEVICES') != uuid or os.environ.get('CUDA_DEVICE_ORDER') != 'PCI_BUS_ID':
        raise Rejected('GPU')
    if os.name == 'nt':
        system = os.environ.get('SystemRoot')
        if not system:
            raise Rejected('GPU')
        expected = Path(system) / 'System32' / 'nvcuda.dll'
        if str(expected).casefold() != str(driver_path).casefold():
            raise Rejected('GPU')
        driver = ctypes.WinDLL(str(expected), winmode=0x00000800)
    else:
        approved = {'/usr/lib/x86_64-linux-gnu/libcuda.so.1', '/usr/lib64/libcuda.so.1'}
        if driver_path not in approved:
            raise Rejected('GPU')
        driver = ctypes.CDLL(driver_path)
    init = driver.cuInit
    init.argtypes, init.restype = [ctypes.c_uint], ctypes.c_int
    if init(0) != 0:
        raise Rejected('GPU')
    count = ctypes.c_int()
    get_count = driver.cuDeviceGetCount
    get_count.argtypes, get_count.restype = [ctypes.POINTER(ctypes.c_int)], ctypes.c_int
    if get_count(ctypes.byref(count)) != 0 or count.value != 1:
        raise Rejected('GPU')
    dev = ctypes.c_int()
    get_device = driver.cuDeviceGet
    get_device.argtypes, get_device.restype = [ctypes.POINTER(ctypes.c_int), ctypes.c_int], ctypes.c_int
    if get_device(ctypes.byref(dev), 0) != 0:
        raise Rejected('GPU')

    class CUuuid(ctypes.Structure):
        _fields_ = [('bytes', ctypes.c_ubyte * 16)]

    observed = CUuuid()
    get_uuid = getattr(driver, 'cuDeviceGetUuid_v2', None)
    if get_uuid is None:
        raise Rejected('GPU')
    get_uuid.argtypes, get_uuid.restype = [ctypes.POINTER(CUuuid), ctypes.c_int], ctypes.c_int
    if get_uuid(ctypes.byref(observed), dev) != 0:
        raise Rejected('GPU')
    import uuid as uuid_module
    actual = 'GPU-' + str(uuid_module.UUID(bytes=bytes(observed.bytes)))
    buffer = ctypes.create_string_buffer(32)
    get_pci = driver.cuDeviceGetPCIBusId
    get_pci.argtypes, get_pci.restype = [ctypes.c_char_p, ctypes.c_int, ctypes.c_int], ctypes.c_int
    if get_pci(buffer, len(buffer), dev) != 0:
        raise Rejected('GPU')
    raw_pci = buffer.value.decode('ascii').lower()
    domain, bus, suffix = raw_pci.split(':')
    actual_pci = domain.zfill(8) + ':' + bus.zfill(2) + ':' + suffix
    if actual != uuid or actual_pci != pci:
        raise Rejected('GPU')
    return driver


class Environment:
    def __init__(self, value, cancelled):
        fields(value, {'profile_id', 'upstream_commit', 'environment_digest',
                       'bundle_root', 'files', 'core_classes', 'modules', 'model_files',
                       'recipe_path', 'recipe_sha256', 'recipe_bytes',
                       'cuda_driver_path', 'gpu_uuid', 'gpu_pci'})
        if value['profile_id'] != PROFILE or value['upstream_commit'] != UPSTREAM_COMMIT:
            raise Rejected('ENVIRONMENT')
        digest(value['environment_digest'])
        self.root = private_directory(value['bundle_root'])
        if self.root != Path(__file__).resolve().parents[2]:
            raise Rejected('ENVIRONMENT')
        self.cancelled = cancelled
        self.files = verify_tree(self.root, value['files'], cancelled)
        self._driver = cuda_identity(value['cuda_driver_path'], value['gpu_uuid'], value['gpu_pci'])
        core = value['core_classes']
        if type(core) is not list or not 1 <= len(core) <= 512 or len(set(core)) != len(core):
            raise Rejected('ENVIRONMENT')
        if any(type(c) is not str or c in DENIED_CLASSES or c in NODE_CLASS_MAPPINGS for c in core):
            raise Rejected('ENVIRONMENT')
        comfy_root = self.root / 'comfy'
        if 'comfy/nodes.py' not in self.files or 'comfy/execution.py' not in self.files:
            raise Rejected('ENVIRONMENT')
        # -I -B is mandatory at the host launcher. No PYTHONPATH/sitecustomize
        # from a user's home or current project is inherited as an import path.
        if not sys.flags.isolated or not sys.dont_write_bytecode or not sys.flags.no_site:
            raise Rejected('ENVIRONMENT')
        sys.path.insert(0, str(comfy_root))
        import comfy.options
        comfy.options.enable_args_parsing(False)
        import comfy.cli_args
        args = comfy.cli_args.args
        args.cpu = False
        args.cuda_device = 0
        args.offline = True
        args.disable_metadata = True
        args.disable_all_custom_nodes = True
        args.disable_api_nodes = True
        args.preview_method = comfy.cli_args.LatentPreviewMethod.NoPreviews
        import torch
        if torch.version.cuda is None or not torch.cuda.is_available() or torch.cuda.device_count() != 1:
            raise Rejected('GPU')
        torch.cuda.set_device(0)
        self.torch = torch
        import nodes
        import folder_paths
        import comfy.model_management as management
        self.nodes, self.management = nodes, management
        original_interrupt = nodes.interrupt_processing

        def interrupt(value=True):
            # Pinned PromptExecutor resets interrupt(False) at entry. An
            # already cancelled original call must survive that reset.
            original_interrupt(bool(value) or cancelled())

        nodes.interrupt_processing = interrupt
        self._models(folder_paths, value['model_files'])
        self.runner = asyncio.Runner()
        with loop_bootstrap():
            self.runner.get_loop()
        self.allowed = set(core)
        modules = value['modules']
        if type(modules) is not list or len(modules) > 64:
            raise Rejected('ENVIRONMENT')
        loaded = set()
        for module in modules:
            fields(module, {'path', 'module_parent', 'classes'})
            rel = relative(module['path'])
            if rel in loaded or module['module_parent'] not in {'comfy_extras', 'custom_nodes'}:
                raise Rejected('ENVIRONMENT')
            loaded.add(rel)
            parent = module['module_parent']
            if not rel.startswith('comfy/' + parent + '/') or (parent == 'comfy_extras' and rel not in self.files):
                raise Rejected('ENVIRONMENT')
            if parent == 'custom_nodes' and rel + '/__init__.py' not in self.files:
                raise Rejected('ENVIRONMENT')
            classes = module['classes']
            if type(classes) is not list or not classes or len(classes) != len(set(classes)) or set(classes) & (self.allowed | set(NODE_CLASS_MAPPINGS) | DENIED_CLASSES):
                raise Rejected('ENVIRONMENT')
            before = dict(nodes.NODE_CLASS_MAPPINGS)
            if not self.runner.run(nodes.load_custom_node(str(self.root / rel), module_parent=parent)):
                raise Rejected('ENVIRONMENT')
            if any(nodes.NODE_CLASS_MAPPINGS.get(k) is not v for k, v in before.items()):
                raise Rejected('ENVIRONMENT')
            if set(nodes.NODE_CLASS_MAPPINGS) - set(before) != set(classes):
                raise Rejected('ENVIRONMENT')
            self.allowed.update(classes)
        self.allowed.update(NODE_CLASS_MAPPINGS)
        if not self.allowed - set(NODE_CLASS_MAPPINGS) <= set(nodes.NODE_CLASS_MAPPINGS):
            raise Rejected('ENVIRONMENT')
        nodes.NODE_CLASS_MAPPINGS.update(NODE_CLASS_MAPPINGS)
        nodes.NODE_CLASS_MAPPINGS = {k: nodes.NODE_CLASS_MAPPINGS[k] for k in self.allowed}
        nodes.EXTENSION_WEB_DIRS.clear()
        recipe_path = relative(value['recipe_path'])
        expected = self.files.get(recipe_path)
        if not recipe_path.startswith('bridge/') or expected is None or expected['sha256'] != value['recipe_sha256'] or expected['bytes'] != value['recipe_bytes'] or expected['bytes'] > 2 << 20:
            raise Rejected('RECIPE')
        self.recipe = Recipe((self.root / recipe_path).read_bytes(), self.allowed)
        # Full file metadata is preparation-only. Do not keep a large Python
        # runtime inventory resident for the entire rental session.
        self.files.clear()

    def _models(self, paths, files):
        categories = {'checkpoints', 'text_encoders', 'diffusion_models', 'vae',
                      'clip_vision', 'controlnet', 'loras', 'embeddings', 'upscale_models'}
        if type(files) is not list or not 1 <= len(files) <= 64:
            raise Rejected('ENVIRONMENT')
        mapping, folders = {}, {}
        for file in files:
            fields(file, {'category', 'alias', 'path', 'bytes', 'sha256'})
            category, alias = file['category'], file['alias']
            if category not in categories or type(alias) is not str or '/' in alias or '\\' in alias or Path(alias).name != alias:
                raise Rejected('ENVIRONMENT')
            key = (category, alias)
            if key in mapping or not os.path.isabs(file['path']):
                raise Rejected('ENVIRONMENT')
            path = Path(file['path'])
            # Parent may contain ordinary read-only model directories, but no
            # symlink/reparse component is imported as an approved model path.
            for parent in path.parents:
                info = parent.lstat()
                if parent.is_symlink() or getattr(info, 'st_file_attributes', 0) & 0x400:
                    raise Rejected('ENVIRONMENT')
            integer(file['bytes'], 1, 200 << 30)
            digest(file['sha256'])
            if path.suffix.lower() != '.safetensors':
                raise Rejected('ENVIRONMENT')
            hash_file(path, file['bytes'], file['sha256'], self.cancelled)
            mapping[key] = str(path)
            roots, extensions = folders.setdefault(category, ([], set()))
            if str(path.parent) not in roots:
                roots.append(str(path.parent))
            extensions.add(path.suffix)
        paths.folder_names_and_paths = folders

        def full(category, alias):
            return mapping.get((category, alias))

        def required(category, alias):
            path = full(category, alias)
            if path is None:
                raise Rejected('RECIPE')
            return path

        paths.get_full_path = full
        paths.get_full_path_or_raise = required
        paths.get_filename_list = lambda category: sorted(alias for cat, alias in mapping if cat == category)

    def synchronize(self):
        self.torch.cuda.synchronize(0)

    def drop_call_state(self):
        # Pinned Comfy keeps a process-global progress registry referencing
        # DynamicPrompt, including runtime node overrides/expanded graphs.
        # CacheType.NONE / executor.reset alone does not clear this owner.
        from comfy_execution.graph import DynamicPrompt
        from comfy_execution.progress import reset_progress_state
        reset_progress_state('', DynamicPrompt({}))

    def close(self):
        self.management.interrupt_current_processing(True)
        self.runner.close()
        self.drop_call_state()
        self.management.unload_all_models()
        self.synchronize()
        self.management.soft_empty_cache(force=True)
        self.recipe.clear()
