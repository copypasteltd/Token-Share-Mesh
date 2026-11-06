"""Original host-owned workspace; reserve encrypted metadata before payloads."""
from __future__ import annotations

import hashlib
import os
import re
import secrets

from .io_guard import ciphertext_write
from .limits import Rejected, canonical, digest, hash_file, private_directory
from .spool import CipherSpool, scope_bytes, wipe


class Workspace:
    def __init__(self, directory, anchor_digest, journal_key, scope, cancelled):
        from cryptography.hazmat.primitives.ciphers.aead import AESGCM
        self.directory = private_directory(directory)
        digest(anchor_digest)
        anchor = self.directory / 'ownership.anchor'
        hash_file(anchor, 32, anchor_digest)
        self._key = bytearray(journal_key)
        if len(self._key) != 32:
            raise Rejected('ENVIRONMENT')
        self._aead = AESGCM(bytes(self._key))
        self._scope = scope_bytes(scope)
        self._cancelled = cancelled
        self._names = set()
        self._spools = []
        self._reserved_plain = 0
        self._closed = False

    def __repr__(self):
        return '<Workspace REDACTED>'

    def reserve(self, name):
        if self._closed or self._cancelled() or name in self._names:
            raise Rejected('CANCELLED')
        if len(self._names) >= 5 or type(name) is not str or re.fullmatch(r'cipher-[a-f0-9]{32}\.bin', name) is None:
            raise Rejected('LIMIT')
        record = canonical({'original_scope_sha256': hashlib.sha256(self._scope).hexdigest(),
                            'owned_cipher_basename': name})
        nonce = secrets.token_bytes(12)
        sealed = nonce + self._aead.encrypt(nonce, record, b'P11_COMFY_WORKSPACE_RESERVATION_1\n' + self._scope)
        with ciphertext_write():
            fd = os.open(self.directory / (name + '.reservation'), os.O_WRONLY |
                         os.O_CREAT | os.O_EXCL | getattr(os, 'O_NOFOLLOW', 0) |
                         getattr(os, 'O_BINARY', 0), 0o600)
            with os.fdopen(fd, 'wb', buffering=0) as target:
                view = memoryview(sealed)
                while view:
                    count = target.write(view)
                    if not count:
                        raise Rejected('UNKNOWN')
                    view = view[count:]
                os.fsync(target.fileno())
            if os.name != 'nt':
                parent = os.open(self.directory, os.O_RDONLY | getattr(os, 'O_DIRECTORY', 0))
                try:
                    os.fsync(parent)
                finally:
                    os.close(parent)
        self._names.add(name)

    def spool(self, scope, limit):
        if scope_bytes(scope) != self._scope or self._closed:
            raise Rejected()
        if type(limit) is not int or limit < 1 or limit > (128 << 20) - self._reserved_plain:
            raise Rejected('LIMIT')
        self._reserved_plain += limit
        spool = CipherSpool(self.directory, scope, limit, self.reserve, self._cancelled)
        self._spools.append(spool)
        return spool

    def close(self):
        if self._closed:
            return
        self._closed = True
        failed = False
        for spool in self._spools:
            try:
                spool.close()
            except BaseException:
                failed = True
        wipe(self._key)
        self._aead = None
        if failed:
            raise Rejected('UNKNOWN')
        # Original host cleans its whole reserved workspace only after actual
        # engine synchronization/join. No discovery, deletion, resume or rerun.
