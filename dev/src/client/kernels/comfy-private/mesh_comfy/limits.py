"""Strict private-wire values. Never include content in exception strings."""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import stat


class Rejected(Exception):
    def __init__(self, code='REJECTED'):
        self.code = code if code in {
            'REJECTED', 'CANCELLED', 'LIMIT', 'FORMAT', 'INTEGRITY',
            'ENVIRONMENT', 'GPU', 'RECIPE', 'UNKNOWN', 'ENCODER',
        } else 'REJECTED'
        super().__init__(self.code)


ID = re.compile(r'^[A-Za-z][A-Za-z0-9_-]{2,127}$')
HASH = re.compile(r'^[a-f0-9]{64}$')
CHUNK = 65536
MAX_MEDIA = 64 << 20


def identifier(value):
    if not isinstance(value, str) or ID.fullmatch(value) is None:
        raise Rejected()
    return value


def digest(value):
    if not isinstance(value, str) or HASH.fullmatch(value) is None:
        raise Rejected()
    return value


def integer(value, lower, upper):
    if type(value) is not int or value < lower or value > upper:
        raise Rejected('LIMIT')
    return value


def fields(value, expected):
    if type(value) is not dict or set(value) != set(expected):
        raise Rejected('FORMAT')
    return value


def canonical(value):
    try:
        return json.dumps(value, ensure_ascii=False, allow_nan=False,
                          sort_keys=True, separators=(',', ':')).encode('utf-8')
    except (TypeError, ValueError, UnicodeError):
        raise Rejected('FORMAT') from None


def strict_json(raw, maximum=2 << 20):
    if not isinstance(raw, bytes) or not 0 < len(raw) <= maximum:
        raise Rejected('LIMIT')

    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise Rejected('FORMAT')
            result[key] = value
        return result

    def constant(_):
        raise Rejected('FORMAT')

    try:
        result = json.loads(raw.decode('utf-8', errors='strict'),
                            object_pairs_hook=pairs, parse_constant=constant)
        # Reject lone escaped surrogates as well as invalid wire UTF-8.
        canonical(result)
        return result
    except (ValueError, UnicodeError, RecursionError):
        raise Rejected('FORMAT') from None


def private_directory(value):
    if type(value) is not str or not os.path.isabs(value):
        raise Rejected('ENVIRONMENT')
    path = Path(value)
    if os.path.normpath(value) != value:
        raise Rejected('ENVIRONMENT')
    for part in (path, *path.parents):
        info = part.lstat()
        if not stat.S_ISDIR(info.st_mode) or stat.S_ISLNK(info.st_mode):
            raise Rejected('ENVIRONMENT')
        if getattr(info, 'st_file_attributes', 0) & 0x400:
            raise Rejected('ENVIRONMENT')
    if os.name != 'nt' and path.stat().st_mode & 0o077:
        raise Rejected('ENVIRONMENT')
    return path


def regular_file(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode):
        raise Rejected('ENVIRONMENT')
    if getattr(info, 'st_file_attributes', 0) & 0x400:
        raise Rejected('ENVIRONMENT')
    return info


def hash_file(path, expected_bytes, expected_hash, cancelled=None):
    regular_file(path)
    h = hashlib.sha256()
    with path.open('rb', buffering=0) as source:
        count = 0
        while block := source.read(CHUNK):
            if cancelled is not None and cancelled():
                raise Rejected('CANCELLED')
            count += len(block)
            if count > expected_bytes:
                raise Rejected('INTEGRITY')
            h.update(block)
    if count != expected_bytes or h.hexdigest() != expected_hash:
        raise Rejected('INTEGRITY')
