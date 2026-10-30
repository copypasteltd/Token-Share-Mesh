"""显式准备固定版本的原生 CUDA/模型验证输入；不推理、不打开监听、不签发设备资格。"""
from __future__ import annotations
import argparse
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[2]
LOCK = ROOT / 'dev/src/client/kernels/llama-worker/qualification.lock.json'
RUNTIME = ROOT / 'runtime/native-llama'

def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as source:
        while block := source.read(1 << 20): h.update(block)
    return h.hexdigest()

def owned(path):
    if not path.resolve().is_relative_to((ROOT / 'runtime').resolve()):
        raise RuntimeError('path outside private runtime')
    for parent in [path, *path.parents]:
        if parent == ROOT: break
        if parent.is_symlink(): raise RuntimeError('symbolic path rejected')
    return path

def fetch(spec, url, accept):
    target = owned(RUNTIME / 'downloads' / spec['name'])
    target.parent.mkdir(parents=True, exist_ok=True)
    if target.exists():
        if target.stat().st_size != spec['size'] or digest(target) != spec['sha256']:
            raise RuntimeError('cached artifact changed: ' + spec['name'])
        return target
    partial = owned(target.with_suffix(target.suffix + '.part'))
    # Never execute an archive or trust a partial download. A failed attempt
    # remains bounded and may be explicitly retried from byte zero.
    request = urllib.request.Request(url, headers={'User-Agent':'Token-Share-Mesh-native-inputs', 'Accept':accept})
    size = 0
    with urllib.request.urlopen(request, timeout=45) as source, partial.open('wb') as output:
        while block := source.read(1 << 20):
            size += len(block)
            if size > spec['size']: raise RuntimeError('download exceeds pinned size')
            output.write(block)
    if size != spec['size'] or digest(partial) != spec['sha256']:
        raise RuntimeError('artifact digest/size rejected: ' + spec['name'])
    partial.replace(target)
    print('verified ' + spec['name'], flush=True)
    return target

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fetch', action='store_true', help='explicitly download about 1.2 GB of frozen inputs')
    parser.add_argument('--model-mirror', choices=['official','hf-mirror'], default='official', help='transport only; the same independently published Qwen SHA-256 is mandatory')
    args = parser.parse_args()
    if not args.fetch: raise RuntimeError('preparation requires explicit --fetch')
    if os.name != 'nt': raise RuntimeError('this frozen package is Windows amd64 only')
    lock = json.loads(LOCK.read_text(encoding='utf-8'))
    tasks = [(a, a['url'], 'application/octet-stream') for a in lock['artifacts']]
    model = lock['model']
    host = 'huggingface.co' if args.model_mirror == 'official' else 'hf-mirror.com'
    base = f"https://{host}/{model['repository']}/resolve/{model['revision']}/"
    tasks.append((model, base + model['name'], 'application/octet-stream'))
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        paths = list(pool.map(lambda t: fetch(*t), tasks))
    package = owned(RUNTIME / 'qwen-0.5b-cuda12')
    package.mkdir(parents=True, exist_ok=True)
    libraries = owned(package / 'lib'); libraries.mkdir(exist_ok=True)
    installed = {}
    licenses = []
    expanded = 0
    for archive in paths[:-1]:
        with zipfile.ZipFile(archive) as source:
            for entry in source.infolist():
                name = PurePosixPath(entry.filename)
                if name.is_absolute() or '..' in name.parts or ':' in entry.filename or '\\' in entry.filename:
                    raise RuntimeError('archive path rejected')
                if entry.is_dir(): continue
                is_dll = name.name.lower() in {'llama.dll','ggml.dll','ggml-base.dll','ggml-cuda.dll','libomp.dll','cublas64_12.dll','cublaslt64_12.dll','cudart64_12.dll'} or name.name.lower().startswith('ggml-cpu-') and name.suffix.lower() == '.dll'
                is_license = any(term in name.name.lower() for term in ['license','licence','copying','notice','eula'])
                if not is_dll and not is_license: continue
                expanded += entry.file_size
                # The pinned upstream ggml-cuda.dll is exactly 594,121,728 B.
                if entry.file_size > 594121728 or expanded > 2 * 1024**3 or (entry.external_attr >> 16) & 0o170000 == 0o120000:
                    raise RuntimeError('archive member rejected')
                target = owned(libraries / name.name) if is_dll else owned(package / 'licenses' / archive.stem / name.name)
                target.parent.mkdir(parents=True, exist_ok=True)
                partial = owned(target.with_suffix(target.suffix + '.extracting'))
                h = hashlib.sha256(); count = 0
                with source.open(entry) as data, partial.open('wb') as output:
                    while block := data.read(1 << 16):
                        count += len(block)
                        if count > entry.file_size: raise RuntimeError('archive member exceeds size')
                        h.update(block); output.write(block)
                if count != entry.file_size: raise RuntimeError('archive member size changed')
                file_hash = h.hexdigest()
                key = str(target).lower()
                if key in installed and installed[key] != file_hash: raise RuntimeError('archive basename collision')
                if target.exists() and digest(target) != file_hash: raise RuntimeError('installed file changed')
                if not target.exists(): partial.replace(target)
                else: partial.unlink()
                installed[key] = file_hash
                if is_license: licenses.append(target.relative_to(package).as_posix())
    model_target = owned(package / model['name'])
    if model_target.exists() and digest(model_target) != model['sha256']: raise RuntimeError('installed model changed')
    if not model_target.exists(): shutil.copyfile(paths[-1], model_target)
    template = owned(package / 'template.txt')
    template_data = model['template_name'].encode()
    if template.exists() and template.read_bytes() != template_data: raise RuntimeError('installed template changed')
    template.write_bytes(template_data)
    license_url = base + 'LICENSE'
    license_path = owned(package / 'licenses/Qwen-APACHE-2.0.txt')
    if license_path.exists(): license_data = license_path.read_bytes()
    else:
        request = urllib.request.Request(license_url, headers={'User-Agent':'Token-Share-Mesh-native-inputs'})
        with urllib.request.urlopen(request, timeout=35) as response: license_data = response.read(65537)
    if len(license_data) > 65536 or hashlib.sha1(b'blob ' + str(len(license_data)).encode() + b'\0' + license_data).hexdigest() != model['license_git_blob_sha1']:
        raise RuntimeError('Qwen license git object rejected')
    license_path.parent.mkdir(exist_ok=True)
    license_path.write_bytes(license_data)
    engine_lock = json.loads((LOCK.parent/'dependencies.lock.json').read_text(encoding='utf-8'))
    engine_license = ROOT/'dev/licenses/llama.cpp-LICENSE'
    if digest(engine_license) != engine_lock['llama']['license_sha256']: raise RuntimeError('llama license changed')
    shutil.copyfile(engine_license, owned(package/'licenses/llama.cpp-MIT.txt'))
    worker = ROOT / 'runtime/native-worker/build/p11-llama-worker.exe'
    if not worker.is_file(): raise RuntimeError('build the pinned project worker first')
    shutil.copyfile(worker, owned(package / worker.name))
    def pin(p): return {'path':p.relative_to(package).as_posix(),'sha256':digest(p)}
    manifest = {'schema':'P11_LLAMA_PACK_1','profile':lock['profile'],'engine_release':lock['engine_release'],
      'executable':pin(package / worker.name),'libraries':[pin(p) for p in sorted(libraries.glob('*.dll'))],
      'library':'lib/llama.dll','backend_registry':'lib/ggml.dll','backend_core':'lib/ggml-base.dll','model':pin(model_target),'template':pin(template)}
    target = owned(package / 'package.json')
    target.write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    receipt = {'schema':'P11_NATIVE_INPUT_PREPARATION_1','lock_sha256':digest(LOCK),'model_transport':host,
       'package':str(target),'package_sha256':digest(target),'libraries':len(manifest['libraries']),
       'licenses':licenses + ['licenses/Qwen-APACHE-2.0.txt','licenses/llama.cpp-MIT.txt'], 'inference_executed':False,'marketplace_approved':False,'virtualization_used':False}
    (RUNTIME / 'preparation.json').write_text(json.dumps(receipt,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(receipt,ensure_ascii=False,indent=2))
    return 0

if __name__ == '__main__': raise SystemExit(main())
