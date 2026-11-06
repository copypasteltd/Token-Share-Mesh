"""Bounded seekable local media ciphertext. Not a network E2E format.

Keys exist only in the owning worker/host. A lost key is unavailability, never
permission to regenerate media. No plaintext file, mounted drive or RAM disk.
"""
from __future__ import annotations

import hashlib
import io
import os
from pathlib import Path
import secrets
import struct
import threading

from .limits import CHUNK, MAX_MEDIA, Rejected, canonical, fields, identifier
from .limits import digest, integer, private_directory, regular_file
from .io_guard import ciphertext_write

MAGIC = b'P11CS01\0'
HEADER = 48
RECORD = struct.Struct('<IBI')
END = struct.Struct('<QQ32s')


def wipe(value):
    if isinstance(value, bytearray):
        value[:] = bytes(len(value))


def scope_bytes(scope):
    fields(scope, {'session_id', 'invocation_id', 'local_generation',
                   'environment_digest', 'recipe_digest'})
    identifier(scope['session_id'])
    identifier(scope['invocation_id'])
    for key in ('environment_digest', 'recipe_digest'):
        digest(scope[key])
    generation = scope['local_generation']
    if type(generation) is not str or not 1 <= len(generation) <= 20 or not generation.isascii() or not generation.isdecimal():
        raise Rejected()
    if str(int(generation)) != generation or not 0 < int(generation) < 1 << 64:
        raise Rejected()
    return canonical(scope)


class CipherSpool(io.RawIOBase):
    """One writer, one bounded seekable reader, immutable authenticated END.

    reserve(name) MUST durably record original workspace ownership before this
    object creates its payload. This is a trusted host callback, not buyer data.
    Each file uses a fresh key/prefix; every record binds original scope/header.
    """
    def __init__(self, directory, scope, limit, reserve, cancelled):
        super().__init__()
        self._lock = threading.RLock()
        self._file = None
        self._reader = None
        self._pending = bytearray()
        self._key = bytearray()
        self._aead = None
        from cryptography.hazmat.primitives.ciphers.aead import AESGCM
        self._directory = private_directory(str(directory))
        self._scope = scope_bytes(scope)
        self._limit = integer(limit, 1, MAX_MEDIA)
        self._cancelled = cancelled
        self._key = bytearray(secrets.token_bytes(32))
        self._aead = AESGCM(bytes(self._key))
        self._header = MAGIC + secrets.token_bytes(8) + hashlib.sha256(self._scope).digest()
        self._pending = bytearray()
        self._hash = hashlib.sha256()
        self._count = 0
        self._size = 0
        self._sealed = False
        self._reader = None
        self._file = None
        self._failed = False
        self._name = 'cipher-' + secrets.token_hex(16) + '.bin'
        self._path = self._directory / self._name
        try:
            if not callable(reserve) or not callable(cancelled):
                raise Rejected()
            reserve(self._name)
            with ciphertext_write():
                fd = os.open(self._path, os.O_RDWR | os.O_CREAT | os.O_EXCL |
                             getattr(os, 'O_NOFOLLOW', 0) | getattr(os, 'O_BINARY', 0), 0o600)
                self._file = os.fdopen(fd, 'w+b', buffering=0)
            self._identity = os.fstat(fd)
            self._write_exact(self._header)
        except BaseException:
            self.close()
            raise

    def __repr__(self):
        return '<CipherSpool REDACTED>'

    def _check(self, sealed=False):
        if self.closed or self._failed or self._file is None or self._aead is None:
            raise Rejected('UNKNOWN')
        if self._cancelled():
            raise Rejected('CANCELLED')
        if sealed and not self._sealed:
            raise Rejected('UNKNOWN')

    def _write_exact(self, data):
        remaining = memoryview(data)
        while remaining:
            written = self._file.write(remaining)
            if not written:
                raise Rejected('UNKNOWN')
            remaining = remaining[written:]

    def _record(self, kind, plain):
        seq = self._count
        if seq >= 1 << 32:
            raise Rejected('LIMIT')
        record = RECORD.pack(seq, kind, len(plain))
        nonce = self._header[8:16] + struct.pack('<I', seq)
        cipher = self._aead.encrypt(nonce, plain, self._header + record)
        self._write_exact(record)
        self._write_exact(cipher)
        self._count += 1

    def writable(self):
        return not self.closed and not self._sealed and not self._failed

    def seekable(self):
        return False

    def tell(self):
        with self._lock:
            self._check()
            return self._size

    def write(self, data):
        view = memoryview(data).cast('B')
        with self._lock:
            self._check()
            if self._sealed or len(view) > self._limit - self._size:
                raise Rejected('LIMIT')
            total = len(view)
            try:
                while view:
                    self._check()
                    count = min(CHUNK - len(self._pending), len(view))
                    self._pending.extend(view[:count])
                    self._hash.update(view[:count])
                    self._size += count
                    view = view[count:]
                    if len(self._pending) == CHUNK:
                        self._record(1, bytes(self._pending))
                        wipe(self._pending)
                        self._pending.clear()
                return total
            except BaseException:
                self._failed = True
                raise

    def flush(self):
        # A codec may flush between chunks. Only seal() emits a short tail/END.
        with self._lock:
            if not self.closed and self._file is not None:
                self._file.flush()

    def seal(self):
        with self._lock:
            self._check()
            if self._sealed or self._size == 0:
                raise Rejected('UNKNOWN')
            try:
                if self._pending:
                    self._record(1, bytes(self._pending))
                    wipe(self._pending)
                    self._pending.clear()
                self._data_count = self._count
                self._plain_digest = self._hash.digest()
                self._record(2, END.pack(self._data_count, self._size, self._plain_digest))
                self._file.flush()
                os.fsync(self._file.fileno())
                self._sealed = True
            except BaseException:
                self._failed = True
                raise

    def _verify_end(self):
        self._check(sealed=True)
        expected_size = HEADER + self._size + self._data_count * (RECORD.size + 16) + RECORD.size + END.size + 16
        path_info = regular_file(self._path)
        info = os.fstat(self._file.fileno())
        if not os.path.samestat(self._identity, info) or not os.path.samestat(path_info, info) or info.st_size != expected_size:
            raise Rejected('INTEGRITY')
        self._file.seek(0)
        if self._file.read(HEADER) != self._header:
            raise Rejected('INTEGRITY')
        self._file.seek(expected_size - RECORD.size - END.size - 16)
        raw = self._file.read(RECORD.size)
        if raw != RECORD.pack(self._data_count, 2, END.size):
            raise Rejected('INTEGRITY')
        nonce = self._header[8:16] + struct.pack('<I', self._data_count)
        try:
            end = self._aead.decrypt(nonce, self._file.read(END.size + 16), self._header + raw)
        except Exception:
            raise Rejected('INTEGRITY') from None
        if end != END.pack(self._data_count, self._size, self._plain_digest):
            raise Rejected('INTEGRITY')

    def reader(self):
        with self._lock:
            if self._reader is not None:
                raise Rejected('UNKNOWN')
            self._verify_end()
            self._reader = CipherReader(self)
            return self._reader

    def _block(self, index):
        self._check(sealed=True)
        if not 0 <= index < self._data_count:
            raise Rejected('INTEGRITY')
        length = min(CHUNK, self._size - index * CHUNK)
        self._file.seek(HEADER + index * (CHUNK + RECORD.size + 16))
        record = self._file.read(RECORD.size)
        if record != RECORD.pack(index, 1, length):
            raise Rejected('INTEGRITY')
        nonce = self._header[8:16] + struct.pack('<I', index)
        try:
            return bytearray(self._aead.decrypt(nonce, self._file.read(length + 16), self._header + record))
        except Exception:
            raise Rejected('INTEGRITY') from None

    def receipt(self):
        """Private host pipe only. Never publish the key/path in provider UI."""
        with self._lock:
            self._verify_end()
            return {'basename': self._name, 'key': bytes(self._key),
                    'scope_sha256': hashlib.sha256(self._scope).hexdigest(),
                    'plaintext_bytes': self._size, 'data_records': self._data_count,
                    'plaintext_sha256': self._plain_digest.hex()}

    def close(self):
        if not hasattr(self, '_lock'):
            return
        with self._lock:
            if self.closed:
                return
            if self._reader is not None:
                self._reader.close()
            wipe(self._pending)
            self._pending.clear()
            wipe(self._key)
            self._aead = None
            try:
                super().close()
            finally:
                if self._file is not None:
                    self._file.close()
                    self._file = None
        # Preserve ciphertext and its reservation until original host cleanup.


class CipherReader(io.RawIOBase):
    def __init__(self, owner):
        super().__init__()
        self._owner = owner
        self._position = 0
        self._index = -1
        self._cache = bytearray()

    def __repr__(self):
        return '<CipherReader REDACTED>'

    def readable(self):
        return not self.closed

    def seekable(self):
        return not self.closed

    def tell(self):
        return self._position

    def seek(self, offset, whence=io.SEEK_SET):
        with self._owner._lock:
            self._owner._check(sealed=True)
            if self.closed or type(offset) is not int:
                raise Rejected('UNKNOWN')
            if whence == io.SEEK_SET:
                position = offset
            elif whence == io.SEEK_CUR:
                position = self._position + offset
            elif whence == io.SEEK_END:
                position = self._owner._size + offset
            else:
                raise Rejected('FORMAT')
            if not 0 <= position <= self._owner._size:
                raise Rejected('LIMIT')
            self._position = position
            return position

    def readinto(self, target):
        with self._owner._lock:
            self._owner._check(sealed=True)
            if self.closed:
                raise Rejected('UNKNOWN')
            view = memoryview(target).cast('B')
            wanted = min(len(view), self._owner._size - self._position)
            done = 0
            while done < wanted:
                index, offset = divmod(self._position, CHUNK)
                if index != self._index:
                    wipe(self._cache)
                    self._cache = self._owner._block(index)
                    self._index = index
                size = min(len(self._cache) - offset, wanted - done)
                if size <= 0:
                    raise Rejected('INTEGRITY')
                view[done:done + size] = memoryview(self._cache)[offset:offset + size]
                done += size
                self._position += size
            return done

    def read(self, size=-1):
        if type(size) is not int or not 0 <= size <= 1 << 20:
            raise Rejected('LIMIT')
        data = bytearray(min(size, self._owner._size - self._position))
        count = self.readinto(data)
        result = bytes(memoryview(data)[:count])
        wipe(data)
        return result

    def close(self):
        with self._owner._lock:
            if not self.closed:
                wipe(self._cache)
                self._cache.clear()
                if self._owner._reader is self:
                    self._owner._reader = None
                super().close()
